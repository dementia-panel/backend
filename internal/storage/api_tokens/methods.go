package api_tokens

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) CreateAPIToken(ctx context.Context, requestID string, name, token string) (APIToken, error) {
	const op = "internal/storage/api_tokens/CreateAPIToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanAPIToken(s.DB.QueryRow(ctx, `INSERT INTO api_tokens(name,token_hash)
		VALUES($1,$2) RETURNING id::text,name,created_at`, name, HashToken(token)))
}
func (s Repository) APITokens(ctx context.Context, requestID string) ([]APIToken, error) {
	const op = "internal/storage/api_tokens/APITokens"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	rows, err := s.DB.Query(ctx, `SELECT id::text,name,created_at FROM api_tokens ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	return pgx.AppendRows([]APIToken{}, rows, func(row pgx.CollectableRow) (APIToken, error) {
		return ScanAPIToken(row)
	})
}
func (s Repository) DeleteAPIToken(ctx context.Context, requestID string, id string) error {
	const op = "internal/storage/api_tokens/DeleteAPIToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	tag, err := s.DB.Exec(ctx, "DELETE FROM api_tokens WHERE id=$1", id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
func (s Repository) APITokenExists(ctx context.Context, requestID string, token string) (bool, error) {
	const op = "internal/storage/api_tokens/APITokenExists"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var exists bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM api_tokens WHERE token_hash=$1)", HashToken(token)).Scan(&exists)
	return exists, err
}
func (s Repository) RenameAPIToken(ctx context.Context, requestID string, id, name string) (APIToken, error) {
	const op = "internal/storage/api_tokens/RenameAPIToken"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	return ScanAPIToken(s.DB.QueryRow(ctx, "UPDATE api_tokens SET name=$2 WHERE id=$1 RETURNING id::text,name,created_at", id, name))
}
