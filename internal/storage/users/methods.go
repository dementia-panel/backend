package users

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) CreateUser(ctx context.Context, requestID string, username string, telegramID *int64, id *string) (User, error) {
	const op = "internal/storage/users/CreateUser"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanUser(s.DB.QueryRow(ctx, `INSERT INTO users(id,username,telegram_id,device_limit) VALUES(coalesce($3::uuid,gen_random_uuid()),$1,$2,(SELECT default_device_limit FROM device_settings WHERE id))
		RETURNING id::text,username,telegram_id,enabled,created_at,updated_at,NULL::timestamptz,false,subscription_plan_id::text,device_limit,0,0`, username, telegramID, id))
}
func (s Repository) CreateUserWithSubscription(ctx context.Context, requestID string, username string, telegramID *int64, planID string, token string, id *string) (User, string, error) {
	const op = "internal/storage/users/CreateUserWithSubscription"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var user User
	var accessID string
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		user, err = ScanUser(tx.QueryRow(ctx, `INSERT INTO users(id,username,telegram_id,device_limit) VALUES(coalesce($3::uuid,gen_random_uuid()),$1,$2,(SELECT default_device_limit FROM device_settings WHERE id))
			RETURNING id::text,username,telegram_id,enabled,created_at,updated_at,NULL::timestamptz,false,subscription_plan_id::text,device_limit,0,0`, username, telegramID, id))
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

func (s Repository) UsersPage(ctx context.Context, requestID string, query UserQuery) (UserPage, error) {
	const op = "internal/storage/users/UsersPage"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(ctx, "listing users")
	result := UserPage{Items: []User{}, Page: query.Page, PerPage: query.PerPage}
	if query.Page < 1 || query.Page > 1000000000 || query.PerPage < 1 || query.PerPage > 100 {
		return result, errors.New("invalid pagination")
	}
	order := "username,id"
	switch query.Sort {
	case "", "username":
	case "last_connected_at":
		order = "last_connected_at DESC NULLS LAST,id"
	default:
		return result, errors.New("invalid user sort")
	}
	if query.Status != "all" && query.Status != "online" && query.Status != "disabled" {
		return result, errors.New("invalid user status")
	}
	const filter = ` WHERE ($1='' OR strpos(lower(username),lower($1))>0 OR strpos(coalesce(telegram_id::text,''),$1)>0)
 AND ($2='all' OR ($2='online' AND online) OR ($2='disabled' AND NOT enabled))`
	source := " FROM (" + SelectUsers + ") listed" + filter
	err := pgx.BeginTxFunc(ctx, s.DB, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE online)"+source, query.Search, query.Status).Scan(&result.Total, &result.OnlineTotal); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT *"+source+" ORDER BY "+order+" LIMIT $3 OFFSET $4", query.Search, query.Status, query.PerPage, int64(query.Page-1)*int64(query.PerPage))
		if err != nil {
			return err
		}
		result.Items, err = pgx.AppendRows([]User{}, rows, func(row pgx.CollectableRow) (User, error) { return ScanUser(row) })
		return err
	})
	result.TotalPages = (result.Total + int64(query.PerPage) - 1) / int64(query.PerPage)
	return result, err
}
