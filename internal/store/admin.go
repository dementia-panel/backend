package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s Store) AdminConfigured(ctx context.Context) (bool, error) {
	var configured bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM administrators)").Scan(&configured)
	return configured, err
}

func (s Store) SetupAdmin(ctx context.Context, username string, salt, hash []byte, issue func(sessionID string) (string, time.Time, error)) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO administrators(username,password_salt,password_hash)
			VALUES($1,$2,$3)`, username, salt, hash); err != nil {
			return err
		}
		return insertAdminSession(ctx, tx, issue)
	})
}

func (s Store) AdminCredentials(ctx context.Context, username string) (salt, hash []byte, err error) {
	err = s.DB.QueryRow(ctx, `SELECT password_salt,password_hash FROM administrators WHERE username=$1`, username).Scan(&salt, &hash)
	return salt, hash, err
}

func (s Store) CreateAdminSession(ctx context.Context, username string, salt, hash []byte, issue func(sessionID string) (string, time.Time, error)) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		// Serialize login with password replacement so a verified old password cannot create a session afterward.
		var id bool
		if err := tx.QueryRow(ctx, "SELECT id FROM administrators WHERE username=$1 AND password_salt=$2 AND password_hash=$3 FOR UPDATE", username, salt, hash).Scan(&id); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, "DELETE FROM admin_sessions WHERE expires_at<=now()"); err != nil {
			return err
		}
		return insertAdminSession(ctx, tx, issue)
	})
}

func insertAdminSession(ctx context.Context, tx pgx.Tx, issue func(string) (string, time.Time, error)) error {
	var id string
	if err := tx.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		return err
	}
	refresh, expires, err := issue(id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admin_sessions(session_id,token_hash,administrator_id,expires_at)
		VALUES($1,$2,true,$3)`, id, HashToken(refresh), expires)
	return err
}

func (s Store) AdminSession(ctx context.Context, id string) (string, error) {
	var username string
	err := s.DB.QueryRow(ctx, `SELECT a.username FROM admin_sessions s
		JOIN administrators a ON a.id=s.administrator_id
		WHERE s.session_id=$1 AND s.expires_at>now()`, id).Scan(&username)
	return username, err
}

func (s Store) RotateAdminSession(ctx context.Context, id, currentRefresh, nextRefresh string, expires time.Time) (bool, error) {
	tag, err := s.DB.Exec(ctx, `UPDATE admin_sessions SET token_hash=$3,expires_at=$4
		WHERE session_id=$1 AND token_hash=$2 AND expires_at>now()`, id, HashToken(currentRefresh), HashToken(nextRefresh), expires)
	return tag.RowsAffected() == 1, err
}

func (s Store) DeleteAdminSession(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM admin_sessions WHERE session_id=$1", id)
	return err
}

func (s Store) DeleteAdminSessionByRefresh(ctx context.Context, token string) error {
	_, err := s.DB.Exec(ctx, "DELETE FROM admin_sessions WHERE token_hash=$1", HashToken(token))
	return err
}

func (s Store) AdminNeedsPasswordChange(ctx context.Context, username string) (bool, error) {
	var required bool
	err := s.DB.QueryRow(ctx, "SELECT password_change_required FROM administrators WHERE username=$1", username).Scan(&required)
	return required, err
}

// CompleteAdminOnboarding replaces a temporary credential and revokes every previous session atomically.
func (s Store) CompleteAdminOnboarding(ctx context.Context, currentUsername, username string, salt, hash []byte, issue func(string) (string, time.Time, error)) error {
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
		return insertAdminSession(ctx, tx, issue)
	})
}
