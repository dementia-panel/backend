package api_tokens

import (
	. "dementia/backend/internal/storage/shared"
)

type Dependencies interface{}
type Repository struct {
	*Connection
	Dependencies
}

func New(connection *Connection, deps Dependencies) *Repository {
	return &Repository{Connection: connection, Dependencies: deps}
}
