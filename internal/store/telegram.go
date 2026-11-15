package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type TelegramNotification struct {
	ID       int64
	Event    string
	Data     json.RawMessage
	Attempts int
}

func (s Store) PendingTelegram(ctx context.Context) ([]TelegramNotification, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,event,data,attempts FROM telegram_notifications
		WHERE next_attempt_at<=now() ORDER BY next_attempt_at,id LIMIT 16`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TelegramNotification])
}

func (s Store) CompleteTelegram(ctx context.Context, id int64) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM telegram_notifications WHERE id=$1", id)
	return err
}

func (s Store) RetryTelegram(ctx context.Context, id int64, delay time.Duration) error {
	_, err := s.DB.Exec(ctx, `UPDATE telegram_notifications SET attempts=attempts+1,
		next_attempt_at=now()+$2::double precision*interval '1 second' WHERE id=$1`, id, delay.Seconds())
	return err
}

type TelegramNodeMetric struct {
	Name     string
	Status   string
	Enabled  bool
	Online   bool
	Received int64
	Sent     int64
}

func (s Store) TelegramNodeMetrics(ctx context.Context) ([]TelegramNodeMetric, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.name,n.status,n.enabled,
		coalesce(n.last_seen_at>now()-interval '1 minute',false),
		coalesce(t.received,0)::bigint,coalesce(t.sent,0)::bigint
		FROM nodes n LEFT JOIN LATERAL (
			SELECT sum(received_bytes) AS received,sum(sent_bytes) AS sent
			FROM traffic_daily WHERE node_id=n.id AND day=(now() AT TIME ZONE 'UTC')::date
		) t ON true WHERE n.retired_at IS NULL ORDER BY n.name,n.id LIMIT 51`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TelegramNodeMetric])
}
