package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	TelegramID      *int64     `json:"telegram_id"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
	Online          bool       `json:"online"`
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.TelegramID, &user.Enabled, &user.CreatedAt, &user.UpdatedAt, &user.LastConnectedAt, &user.Online)
	return user, err
}

func (s Store) CreateUser(ctx context.Context, username string, telegramID *int64) (User, error) {
	return scanUser(s.DB.QueryRow(ctx, `INSERT INTO users(username,telegram_id) VALUES($1,$2)
		RETURNING id::text,username,telegram_id,enabled,created_at,updated_at,NULL::timestamptz,false`, username, telegramID))
}

const selectUsers = `SELECT u.id::text,u.username,u.telegram_id,u.enabled,u.created_at,u.updated_at,
	activity.last_connected_at,coalesce(activity.online,false)
	FROM users u
	CROSS JOIN LATERAL (
		SELECT greatest(max(p.last_handshake_at),max(s.removed_peer_last_handshake_at)) AS last_connected_at,
		    bool_or(u.enabled AND s.enabled AND s.expires_at>now() AND (s.traffic_limit_bytes IS NULL OR s.traffic_used_bytes<s.traffic_limit_bytes) AND n.enabled AND n.status='ready'
		        AND n.applied_revision=n.revision
		        AND n.handshakes_reported_at>now()-interval '1 minute'
		        AND p.current_handshake_at>now()-interval '3 minutes') AS online
		FROM subscriptions s LEFT JOIN peers p ON p.subscription_id=s.id LEFT JOIN nodes n ON n.id=p.node_id
		WHERE s.user_id=u.id
	) activity`

func (s Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.DB.Query(ctx, selectUsers+" ORDER BY u.username")
	if err != nil {
		return nil, err
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if users == nil && err == nil {
		users = []User{}
	}
	return users, err
}

func (s Store) User(ctx context.Context, id string) (User, error) {
	return scanUser(s.DB.QueryRow(ctx, selectUsers+" WHERE u.id=$1", id))
}

func (s Store) UpdateUser(ctx context.Context, id string, username *string, telegramID *int64, setTelegram bool, enabled *bool) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var wasEnabled bool
		if err := tx.QueryRow(ctx, `SELECT enabled FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&wasEnabled); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET username=coalesce($2,username),
		    telegram_id=CASE WHEN $3 THEN $4 ELSE telegram_id END,enabled=coalesce($5,enabled),updated_at=now()
		    WHERE id=$1 AND (username IS DISTINCT FROM coalesce($2,username)
		        OR ($3 AND telegram_id IS DISTINCT FROM $4) OR enabled IS DISTINCT FROM coalesce($5,enabled))`,
			id, username, setTelegram, telegramID, enabled)
		if err != nil || enabled == nil || *enabled == wasEnabled {
			return err
		}
		return bumpNodes(ctx, tx, `SELECT p.node_id FROM peers p JOIN subscriptions s ON s.id=p.subscription_id WHERE s.user_id=$1`, id)
	})
}
