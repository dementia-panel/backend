package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type TrafficCounter struct {
	Received int64 `json:"received"`
	Sent     int64 `json:"sent"`
}

func recordTraffic(ctx context.Context, tx pgx.Tx, nodeID string, counters map[string]TrafficCounter) error {
	if len(counters) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `WITH samples AS (
		SELECT key,(value->>'received')::bigint AS rx,(value->>'sent')::bigint AS tx
		FROM jsonb_each($2::jsonb)
	), deltas AS MATERIALIZED (
		SELECT p.public_key,p.subscription_id,s.rx,s.tx,
			CASE WHEN s.rx>=p.received_counter THEN s.rx-p.received_counter ELSE s.rx END AS rx_delta,
			CASE WHEN s.tx>=p.sent_counter THEN s.tx-p.sent_counter ELSE s.tx END AS tx_delta
		FROM samples s JOIN peers p ON p.public_key=s.key
		WHERE p.node_id=$1 AND s.rx>=0 AND s.tx>=0
	), counters AS (
		UPDATE peers p SET received_counter=d.rx,sent_counter=d.tx FROM deltas d
		WHERE p.public_key=d.public_key AND (p.received_counter,p.sent_counter) IS DISTINCT FROM (d.rx,d.tx)
	), usage AS (
		UPDATE subscriptions s SET traffic_used_bytes=s.traffic_used_bytes+d.bytes FROM (
			SELECT subscription_id,sum(rx_delta+tx_delta)::bigint AS bytes FROM deltas
			GROUP BY subscription_id HAVING sum(rx_delta+tx_delta)>0
		) d WHERE s.id=d.subscription_id RETURNING s.id
	)
	INSERT INTO traffic_daily(day,node_id,user_id,received_bytes,sent_bytes)
	SELECT (now() AT TIME ZONE 'UTC')::date,$1,s.user_id,sum(d.rx_delta),sum(d.tx_delta)
	FROM deltas d JOIN subscriptions s ON s.id=d.subscription_id JOIN usage u ON u.id=s.id GROUP BY s.user_id
	ON CONFLICT(node_id,day,user_id) DO UPDATE
	SET received_bytes=traffic_daily.received_bytes+excluded.received_bytes,
	    sent_bytes=traffic_daily.sent_bytes+excluded.sent_bytes`, nodeID, counters)
	return err
}

type DailyTraffic struct {
	Day      string `json:"day"`
	Received int64  `json:"received_bytes"`
	Sent     int64  `json:"sent_bytes"`
}

func (s Store) TrafficHistory(ctx context.Context, nodeID, userID string, days int) ([]DailyTraffic, error) {
	rows, err := s.DB.Query(ctx, `SELECT day::text,sum(received_bytes)::bigint,sum(sent_bytes)::bigint
		FROM traffic_daily WHERE day >= (now() AT TIME ZONE 'UTC')::date-$3::integer+1
		AND (nullif($1,'')::uuid IS NULL OR node_id=nullif($1,'')::uuid)
		AND (nullif($2,'')::uuid IS NULL OR user_id=nullif($2,'')::uuid)
		GROUP BY day ORDER BY day`, nodeID, userID, days)
	if err != nil {
		return nil, err
	}
	return pgx.AppendRows([]DailyTraffic{}, rows, pgx.RowToStructByPos[DailyTraffic])
}

func (s Store) SetTrafficLimit(ctx context.Context, id string, limit *int64, reset bool) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var subscription string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM subscriptions WHERE id=$1 FOR UPDATE", id).Scan(&subscription); err != nil {
			return err
		}
		if err := bumpNodes(ctx, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE subscriptions SET traffic_limit_bytes=CASE WHEN $2 THEN traffic_limit_bytes ELSE $3 END,
			traffic_used_bytes=CASE WHEN $2 THEN 0 ELSE traffic_used_bytes END WHERE id=$1`, id, reset, limit)
		return err
	})
}

func (s Store) SetTrafficResetSchedule(ctx context.Context, id string, days *int) error {
	tag, err := s.DB.Exec(ctx, `UPDATE subscriptions SET traffic_reset_days=$2,
		traffic_reset_at=now()+$2::integer*interval '24 hours' WHERE id=$1`, id, days)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s Store) ResetDueTraffic(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text FROM subscriptions WHERE traffic_reset_at<=now()
			ORDER BY id LIMIT 250 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(ids) == 0 {
			return err
		}
		if _, err := tx.Exec(ctx, `WITH affected AS (
			SELECT id FROM nodes WHERE id IN (
				SELECT node_id FROM peers WHERE subscription_id=ANY($1::text[]::uuid[])
			) ORDER BY id FOR UPDATE
		) UPDATE nodes n SET revision=n.revision+1 FROM affected a WHERE n.id=a.id`, ids); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `WITH reset AS (
			UPDATE subscriptions SET traffic_used_bytes=0,traffic_reset_at=now()+traffic_reset_days*interval '24 hours'
			WHERE id=ANY($1::text[]::uuid[]) RETURNING id,user_id
		) INSERT INTO events(scope,event,data)
		SELECT 'subscription','subscription.traffic_reset',jsonb_build_object('id',id,'user_id',user_id)
		FROM reset WHERE current_setting('dementia.webhook_enabled',true)='on'`, ids)
		return err
	})
}
