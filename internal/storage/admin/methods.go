package admin

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Repository) AdminConfigured(ctx context.Context, requestID string) (bool, error) {
	const op = "internal/storage/admin/AdminConfigured"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var configured bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM administrators)").Scan(&configured)
	return configured, err
}
func (s Repository) SetupAdmin(ctx context.Context, requestID string, username string, salt, hash []byte, issue func(sessionID string) (string, time.Time, error)) error {
	const op = "internal/storage/admin/SetupAdmin"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO administrators(username,password_salt,password_hash)
			VALUES($1,$2,$3)`, username, salt, hash); err != nil {
			return err
		}
		return InsertAdminSession(ctx, requestID, tx, issue)
	})
}
func (s Repository) AdminCredentials(ctx context.Context, requestID string, username string) (salt, hash []byte, err error) {
	const op = "internal/storage/admin/AdminCredentials"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	err = s.DB.QueryRow(ctx, `SELECT password_salt,password_hash FROM administrators WHERE username=$1`, username).Scan(&salt, &hash)
	return salt, hash, err
}
func (s Repository) CreateAdminSession(ctx context.Context, requestID string, username string, salt, hash []byte, issue func(sessionID string) (string, time.Time, error)) error {
	const op = "internal/storage/admin/CreateAdminSession"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var id bool
		if err := tx.QueryRow(ctx, "SELECT id FROM administrators WHERE username=$1 AND password_salt=$2 AND password_hash=$3 FOR UPDATE", username, salt, hash).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM admin_sessions WHERE expires_at<=now()"); err != nil {
			return err
		}
		return InsertAdminSession(ctx, requestID, tx, issue)
	})
}
func (s Repository) AdminSession(ctx context.Context, requestID string, id string) (string, error) {
	const op = "internal/storage/admin/AdminSession"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var username string
	err := s.DB.QueryRow(ctx, `SELECT a.username FROM admin_sessions s
		JOIN administrators a ON a.id=s.administrator_id
		WHERE s.session_id=$1 AND s.expires_at>now()`, id).Scan(&username)
	return username, err
}
func (s Repository) RotateAdminSession(ctx context.Context, requestID string, id, currentRefresh, nextRefresh string, expires time.Time) (bool, error) {
	const op = "internal/storage/admin/RotateAdminSession"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, `UPDATE admin_sessions SET token_hash=$3,expires_at=$4
		WHERE session_id=$1 AND token_hash=$2 AND expires_at>now()`, id, HashToken(currentRefresh), HashToken(nextRefresh), expires)
	return tag.RowsAffected() == 1, err
}
func (s Repository) DeleteAdminSession(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/admin/DeleteAdminSession"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, "DELETE FROM admin_sessions WHERE session_id=$1", id)
	return err
}
func (s Repository) DeleteAdminSessionByRefresh(ctx context.Context, requestID string, token string) error {
	const op = "internal/storage/admin/DeleteAdminSessionByRefresh"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, "DELETE FROM admin_sessions WHERE token_hash=$1", HashToken(token))
	return err
}
func (s Repository) AdminNeedsPasswordChange(ctx context.Context, requestID string, username string) (bool, error) {
	const op = "internal/storage/admin/AdminNeedsPasswordChange"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var required bool
	err := s.DB.QueryRow(ctx, "SELECT password_change_required FROM administrators WHERE username=$1", username).Scan(&required)
	return required, err
}
func (s Repository) CompleteAdminOnboarding(ctx context.Context, requestID string, currentUsername, username string, salt, hash []byte, issue func(string) (string, time.Time, error)) error {
	const op = "internal/storage/admin/CompleteAdminOnboarding"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		var required bool
		if err := tx.QueryRow(ctx, "SELECT password_change_required FROM administrators WHERE username=$1 FOR UPDATE", currentUsername).Scan(&required); err != nil {
			return err
		}
		if !required {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(ctx, "UPDATE administrators SET username=$1,password_salt=$2,password_hash=$3,password_change_required=false", username, salt, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM admin_sessions"); err != nil {
			return err
		}
		return InsertAdminSession(ctx, requestID, tx, issue)
	})
}
