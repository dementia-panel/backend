package telegram

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Repository) PendingTelegram(ctx context.Context, requestID string) ([]TelegramNotification, error) {
	const op = "internal/storage/telegram/PendingTelegram"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT id,event,data,attempts FROM telegram_notifications
		WHERE next_attempt_at<=now() ORDER BY next_attempt_at,id LIMIT 16`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TelegramNotification])
}
func (s Repository) CompleteTelegram(ctx context.Context, requestID string, id int64) error {
	const op = "internal/storage/telegram/CompleteTelegram"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, "DELETE FROM telegram_notifications WHERE id=$1", id)
	return err
}
func (s Repository) RetryTelegram(ctx context.Context, requestID string, id int64, delay time.Duration) error {
	const op = "internal/storage/telegram/RetryTelegram"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `UPDATE telegram_notifications SET attempts=attempts+1,
		next_attempt_at=now()+$2::double precision*interval '1 second' WHERE id=$1`, id, delay.Seconds())
	return err
}
func (s Repository) TelegramNodeMetrics(ctx context.Context, requestID string) ([]TelegramNodeMetric, error) {
	const op = "internal/storage/telegram/TelegramNodeMetrics"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
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
func (s Repository) TelegramSettings(ctx context.Context, requestID string) (TelegramSettings, error) {
	const op = "internal/storage/telegram/TelegramSettings"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var result TelegramSettings
	var raw []byte
	err := s.DB.QueryRow(ctx, "SELECT categories FROM telegram_settings WHERE id=true").Scan(&raw)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func (s Repository) SaveTelegramSettings(ctx context.Context, requestID string, value TelegramSettings) error {
	const op = "internal/storage/telegram/SaveTelegramSettings"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE telegram_settings SET categories=$1 WHERE id=true", raw); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM telegram_notifications WHERE NOT coalesce(($1::jsonb->>split_part(event,'.',1))::boolean,false)`, raw)
		return err
	})
}
