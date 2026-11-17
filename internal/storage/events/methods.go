package events

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Repository) StartConnectionCheck(ctx context.Context, requestID string) (ConnectionCheck, error) {
	const op = "internal/storage/events/StartConnectionCheck"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var check ConnectionCheck
	err := s.DB.QueryRow(ctx, `SELECT statement_timestamp(),
		ARRAY(SELECT id::text FROM nodes WHERE status='ready' AND retired_at IS NULL)`).Scan(&check.Started, &check.NodeIDs)
	return check, err
}
func (s Repository) FinishConnectionCheck(ctx context.Context, requestID string, check ConnectionCheck) ([]ConnectionChange, error) {
	const op = "internal/storage/events/FinishConnectionCheck"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `WITH observed AS MATERIALIZED (
		SELECT id,name,connection_ok,coalesce(last_seen_at >= $1,false) AS online
		FROM nodes WHERE id=ANY($2::text[]::uuid[]) AND status='ready' AND retired_at IS NULL
		AND (connection_checked_at IS NULL OR connection_checked_at < $1)
		FOR UPDATE
	), updated AS (
		UPDATE nodes n SET connection_ok=o.online,connection_checked_at=statement_timestamp()
		FROM observed o WHERE n.id=o.id RETURNING n.id
	), changed AS (
		SELECT o.id,o.name,o.online FROM observed o JOIN updated u USING(id)
		WHERE o.connection_ok IS DISTINCT FROM o.online AND (o.connection_ok IS NOT NULL OR NOT o.online)
	), queued AS (
		INSERT INTO events(scope,event,data)
		SELECT 'node',CASE WHEN online THEN 'node.connection_restored' ELSE 'node.connection_lost' END,
		    jsonb_build_object('id',id,'name',name,'online',online,'message',
		        CASE WHEN online THEN 'Связь с нодой восстановлена' ELSE 'Нода не ответила на проверку связи' END)
		FROM changed WHERE current_setting('dementia.webhook_enabled', true)='on'
		    OR current_setting('dementia.telegram_enabled', true)='on'
	) SELECT id::text,name,online FROM changed`, check.Started, check.NodeIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ConnectionChange, error) {
		var change ConnectionChange
		err := row.Scan(&change.ID, &change.Name, &change.Online)
		return change, err
	})
}
func (s Repository) PendingEvents(ctx context.Context, requestID string) ([]Event, error) {
	const op = "internal/storage/events/PendingEvents"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT id,scope,event,created_at,data,attempts FROM events
		WHERE delivered_at IS NULL AND next_attempt_at <= now()
		ORDER BY next_attempt_at,id LIMIT 32`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var event Event
		err := row.Scan(&event.ID, &event.Scope, &event.Name, &event.Timestamp, &event.Data, &event.Attempts)
		return event, err
	})
}
func (s Repository) MarkEventDelivered(ctx context.Context, requestID string, id int64) error {
	const op = "internal/storage/events/MarkEventDelivered"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, "UPDATE events SET delivered_at=now() WHERE id=$1", id)
	return err
}
func (s Repository) RetryEvent(ctx context.Context, requestID string, id int64, delay time.Duration) error {
	const op = "internal/storage/events/RetryEvent"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `UPDATE events SET attempts=attempts+1,
		next_attempt_at=now()+$2::double precision*interval '1 second' WHERE id=$1`, id, delay.Seconds())
	return err
}
func (s Repository) PruneDeliveredEvents(ctx context.Context, requestID string) error {
	const op = "internal/storage/events/PruneDeliveredEvents"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `DELETE FROM events WHERE id IN (
		SELECT id FROM events WHERE delivered_at < now()-interval '7 days'
		ORDER BY delivered_at LIMIT 1000
	)`)
	return err
}
