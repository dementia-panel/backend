package sl

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
)

func Err(err error) slog.Attr {
	if err == nil {
		return slog.Any("error", nil)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return slog.String("error", "PostgreSQL error: "+pgErr.Code)
	}
	return slog.Attr{Key: "error", Value: slog.StringValue(err.Error())}
}
