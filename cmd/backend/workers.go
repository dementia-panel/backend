package main

import (
	"context"
	"dementia/backend/internal/infrastructure/config"
	"dementia/backend/internal/infrastructure/postgresql"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/store"
	"dementia/backend/internal/telegram"
	"dementia/backend/internal/worker"
	"log/slog"
	"sync"
	"time"
)

func startWorkers(ctx context.Context, stop context.CancelFunc, database *postgresql.Database, dbStore store.Store, c *config.Config, bot *telegram.Bot, webhookStatus *integration.Tracker) *sync.WaitGroup {
	workers := &sync.WaitGroup{}
	workers.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe, cancel := context.WithTimeout(ctx, 3*time.Second)
				err := database.Ownership.Ping(probe)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Error("PostgreSQL ownership connection lost", sl.Err(err))
					stop()
					return
				}
			}
		}
	})
	workers.Go(func() {
		worker.MonitorNodes(ctx, dbStore, c.CheckInterval)
	})
	workers.Go(func() {
		worker.ResetTraffic(ctx, dbStore)
	})
	if bot != nil {
		workers.Go(func() {
			bot.Run(ctx, c.PublicURL)
		})
		workers.Go(func() {
			bot.DeliverNotifications(ctx)
		})
	}
	if c.WebhookURL != "" {
		workers.Go(func() {
			worker.DeliverWebhooks(ctx, dbStore, c.WebhookURL, c.WebhookSecret, webhookStatus)
		})
	}
	return workers
}
