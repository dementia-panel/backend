package traffic

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) TrafficHistory(ctx context.Context, requestID string, nodeID, userID string, days int) ([]DailyTraffic, error) {
	const op = "internal/storage/traffic/TrafficHistory"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
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
func (s Repository) SetTrafficLimit(ctx context.Context, requestID string, id string, limit *int64, reset bool) error {
	const op = "internal/storage/traffic/SetTrafficLimit"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var subscription string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM subscriptions WHERE id=$1 FOR UPDATE", id).Scan(&subscription); err != nil {
			return err
		}
		if err := BumpNodes(ctx, requestID, tx, "SELECT node_id FROM peers WHERE subscription_id=$1", id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE subscriptions SET traffic_limit_bytes=CASE WHEN $2 THEN traffic_limit_bytes ELSE $3 END,
			traffic_used_bytes=CASE WHEN $2 THEN 0 ELSE traffic_used_bytes END WHERE id=$1`, id, reset, limit)
		return err
	})
}
func (s Repository) SetTrafficResetSchedule(ctx context.Context, requestID string, id string, days *int) error {
	const op = "internal/storage/traffic/SetTrafficResetSchedule"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE subscriptions SET traffic_reset_days=$2,
		traffic_reset_at=now()+$2::integer*interval '24 hours' WHERE id=$1`, id, days)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s Repository) ResetDueTraffic(ctx context.Context, requestID string) error {
	const op = "internal/storage/traffic/ResetDueTraffic"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
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
