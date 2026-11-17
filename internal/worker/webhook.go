package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/store"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func DeliverWebhooks(ctx context.Context, repo store.Store, endpoint, secret string, status *integration.Tracker) {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	defer client.CloseIdleConnections()
	nextPrune := time.Time{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if time.Now().After(nextPrune) {
			pruneCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := repo.PruneDeliveredEvents(pruneCtx, middleware.GetReqID(pruneCtx))
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Error("could not prune delivered webhooks", sl.Err(err))
			}
			nextPrune = time.Now().Add(time.Minute)
		}
		batchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		events, err := repo.PendingEvents(batchCtx, middleware.GetReqID(batchCtx))
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Error("could not read webhook queue", sl.Err(err))
		}
		for _, event := range events {
			if ctx.Err() != nil {
				return
			}
			if err := sendWebhook(ctx, client, endpoint, secret, event); err != nil {
				if ctx.Err() != nil {
					return
				}
				if status != nil {
					status.Record(false)
				}
				delay := min(time.Second<<min(event.Attempts, 10), 15*time.Minute)
				slog.Warn("webhook delivery failed", "event_id", event.ID, "event", event.Name, sl.Err(err), "retry_in", delay)
				updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err = repo.RetryEvent(updateCtx, middleware.GetReqID(updateCtx), event.ID, delay)
				cancel()
				if err != nil {
					slog.Error("could not reschedule webhook", "event_id", event.ID, sl.Err(err))
				}
				continue
			}
			if status != nil {
				status.Record(true)
			}
			updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			err := repo.MarkEventDelivered(updateCtx, middleware.GetReqID(updateCtx), event.ID)
			cancel()
			if err != nil {
				slog.Error("could not record webhook delivery", "event_id", event.ID, sl.Err(err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func sendWebhook(ctx context.Context, client *http.Client, endpoint, secret string, event store.Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not construct webhook request")
	}
	signature := hmac.New(sha256.New, []byte(secret))
	signature.Write(body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Remnawave-Signature", hex.EncodeToString(signature.Sum(nil)))
	req.Header.Set("X-Remnawave-Timestamp", event.Timestamp.UTC().Format(time.RFC3339Nano))
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("receiver returned HTTP %d", res.StatusCode)
	}
	return nil
}
