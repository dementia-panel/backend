package worker

import (
	"context"
	"log/slog"
	"time"

	"dementia/backend/internal/store"
)

func MonitorNodes(ctx context.Context, repo store.Store, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := checkNodes(ctx, repo); err != nil && ctx.Err() == nil {
			slog.Error("node monitoring failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func checkNodes(ctx context.Context, repo store.Store) error {
	check, err := repo.StartConnectionCheck(ctx)
	if err != nil || len(check.NodeIDs) == 0 {
		return err
	}
	// Allow a full agent poll and request cycle for a fresh acknowledgment.
	timeout := time.NewTimer(12 * time.Second)
	defer timeout.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
	}
	changes, err := repo.FinishConnectionCheck(ctx, check)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if change.Online {
			slog.Info("node connection restored", "node_id", change.ID, "name", change.Name)
		} else {
			slog.Error("node did not respond", "node_id", change.ID, "name", change.Name)
		}
	}
	return nil
}
