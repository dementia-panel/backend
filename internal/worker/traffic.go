package worker

import (
	"context"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/store"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func ResetTraffic(ctx context.Context, repo store.Store) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := repo.ResetDueTraffic(ctx, middleware.GetReqID(ctx)); err != nil && ctx.Err() == nil {
			slog.Error("scheduled traffic reset failed", sl.Err(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
