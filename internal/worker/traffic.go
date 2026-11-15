package worker

import (
	"context"
	"log/slog"
	"time"

	"dementia/backend/internal/store"
)

func ResetTraffic(ctx context.Context, repo store.Store) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := repo.ResetDueTraffic(ctx); err != nil && ctx.Err() == nil {
			slog.Error("scheduled traffic reset failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
