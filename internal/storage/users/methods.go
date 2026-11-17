package users

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) CreateUser(ctx context.Context, requestID string, username string, telegramID *int64) (User, error) {
	const op = "internal/storage/users/CreateUser"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanUser(s.DB.QueryRow(ctx, `INSERT INTO users(username,telegram_id,device_limit) VALUES($1,$2,(SELECT default_device_limit FROM device_settings WHERE id))
		RETURNING id::text,username,telegram_id,enabled,created_at,updated_at,NULL::timestamptz,false,subscription_plan_id::text,device_limit,0,0`, username, telegramID))
}
func (s Repository) CreateUserWithSubscription(ctx context.Context, requestID string, username string, telegramID *int64, planID string, token string) (User, string, error) {
	const op = "internal/storage/users/CreateUserWithSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var user User
	var accessID string
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		user, err = ScanUser(tx.QueryRow(ctx, `INSERT INTO users(username,telegram_id,device_limit) VALUES($1,$2,(SELECT default_device_limit FROM device_settings WHERE id))
			RETURNING id::text,username,telegram_id,enabled,created_at,updated_at,NULL::timestamptz,false,subscription_plan_id::text,device_limit,0,0`, username, telegramID))
		if err != nil {
			return err
		}
		if accessID, _, err = s.AssignSubscriptionPlanTx(ctx, requestID, tx, user.ID, &planID, token); err != nil {
			return err
		}
		user.SubscriptionPlanID = &planID
		user.DeviceCount = 1
		return nil
	})
	return user, accessID, err
}
func (s Repository) Users(ctx context.Context, requestID string) ([]User, error) {
	const op = "internal/storage/users/Users"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, SelectUsers+" ORDER BY u.username")
	if err != nil {
		return nil, err
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) {
		return ScanUser(row)
	})
	if users == nil && err == nil {
		users = []User{}
	}
	return users, err
}
func (s Repository) User(ctx context.Context, requestID string, id string) (User, error) {
	const op = "internal/storage/users/User"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanUser(s.DB.QueryRow(ctx, SelectUsers+" WHERE u.id=$1", id))
}
func (s Repository) UpdateUser(ctx context.Context, requestID string, id string, username *string, telegramID *int64, setTelegram bool, enabled *bool) error {
	const op = "internal/storage/users/UpdateUser"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var wasEnabled bool
		if err := tx.QueryRow(ctx, `SELECT enabled FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&wasEnabled); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET username=coalesce($2,username),
		    telegram_id=CASE WHEN $3 THEN $4 ELSE telegram_id END,enabled=coalesce($5,enabled),updated_at=now()
		    WHERE id=$1 AND (username IS DISTINCT FROM coalesce($2,username)
		        OR ($3 AND telegram_id IS DISTINCT FROM $4) OR enabled IS DISTINCT FROM coalesce($5,enabled))`, id, username, setTelegram, telegramID, enabled)
		if err != nil || enabled == nil || *enabled == wasEnabled {
			return err
		}
		return BumpNodes(ctx, requestID, tx, `SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.user_id=$1`, id)
	})
}
