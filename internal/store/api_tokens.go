package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type APIToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func scanAPIToken(row pgx.Row) (APIToken, error) {
	var token APIToken
	err := row.Scan(&token.ID, &token.Name, &token.CreatedAt)
	return token, err
}

func (s Store) CreateAPIToken(ctx context.Context, name, token string) (APIToken, error) {
	return scanAPIToken(s.DB.QueryRow(ctx, `INSERT INTO api_tokens(name,token_hash)
		VALUES($1,$2) RETURNING id::text,name,created_at`, name, HashToken(token)))
}

func (s Store) APITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.DB.Query(ctx, `SELECT id::text,name,created_at FROM api_tokens ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	return pgx.AppendRows([]APIToken{}, rows, func(row pgx.CollectableRow) (APIToken, error) {
		return scanAPIToken(row)
	})
}

func (s Store) DeleteAPIToken(ctx context.Context, id string) error {
	tag, err := s.DB.Exec(ctx, "DELETE FROM api_tokens WHERE id=$1", id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s Store) APITokenExists(ctx context.Context, token string) (bool, error) {
	var exists bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM api_tokens WHERE token_hash=$1)", HashToken(token)).Scan(&exists)
	return exists, err
}
