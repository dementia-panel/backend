package devices

import (
	"context"
	. "dementia/backend/internal/storage/shared"
	"dementia/backend/internal/vpn"

	"github.com/jackc/pgx/v5"
)

type Dependencies interface {
	InsertDevicePeer(ctx context.Context, requestID string, tx pgx.Tx, subscriptionID, nodeID, deviceID string) error
	ClientProfile(row pgx.Row, requestID string) (vpn.ClientProfile, error)
}
type Repository struct {
	*Connection
	Dependencies
}

func New(connection *Connection, deps Dependencies) *Repository {
	return &Repository{Connection: connection, Dependencies: deps}
}
