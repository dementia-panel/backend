package plans

import (
	"context"
	. "dementia/backend/internal/storage/shared"

	"github.com/jackc/pgx/v5"
)

type Dependencies interface {
	ReplaceSubscriptionNodesTx(ctx context.Context, requestID string, tx pgx.Tx, subscriptionID string, nodeIDs []string) error
	EnsureInitialDevice(ctx context.Context, requestID string, tx pgx.Tx, userID string) error
}
type Repository struct {
	*Connection
	Dependencies
}

func New(connection *Connection, deps Dependencies) *Repository {
	return &Repository{Connection: connection, Dependencies: deps}
}
