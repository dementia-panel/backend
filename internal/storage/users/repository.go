package users

import (
	"context"
	. "dementia/backend/internal/storage/shared"

	"github.com/jackc/pgx/v5"
)

type Dependencies interface {
	AssignSubscriptionPlanTx(ctx context.Context, requestID string, tx pgx.Tx, userID string, planID *string, token string) (id string, issued bool, err error)
}
type Repository struct {
	*Connection
	Dependencies
}

func New(connection *Connection, deps Dependencies) *Repository {
	return &Repository{Connection: connection, Dependencies: deps}
}
