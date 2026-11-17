package telegram_test

import (
	"context"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"strings"
	"testing"
	"time"
)

func TestCategorySelectionSuppressesNotifications(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	settings := store.TelegramSettings{Admin: true, APIToken: true}
	if err := repo.SaveTelegramSettings(ctx, requestID, settings); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateUser(ctx, requestID, "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = user
	token, err := repo.CreateAPIToken(ctx, requestID, "Service", "token-that-must-not-be-notified")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RenameAPIToken(ctx, requestID, token.ID, "Updated"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAPIToken(ctx, requestID, token.ID); err != nil {
		t.Fatal(err)
	}
	events, err := repo.PendingTelegram(ctx, requestID)
	if err != nil || len(events) != 3 {
		t.Fatalf("notifications: %+v %v", events, err)
	}
	for _, event := range events {
		if !strings.HasPrefix(event.Event, "api_token.") || strings.Contains(string(event.Data), "token-that-must-not-be-notified") {
			t.Fatal("wrong event or secret exposure")
		}
	}
	if err := repo.SaveTelegramSettings(ctx, requestID, store.TelegramSettings{}); err != nil {
		t.Fatal(err)
	}
	events, err = repo.PendingTelegram(ctx, requestID)
	if err != nil || len(events) != 0 {
		t.Fatal("disabled notifications remain queued", err)
	}
}

func TestConcurrentCategoryChangeBlocksStaleEnqueue(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, _ := vpn.NewVault(make([]byte, 32))
	repo := store.New(pool, vault)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var owner int
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE telegram_settings SET categories='{"node":false,"user":false,"admin":false,"api_token":false,"subscription":false}'::jsonb WHERE id=true`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := repo.ReserveNode(ctx, "concurrent-test", "Node", "203.0.113.12", "agent-token", 22, 51820)
		done <- err
	}()
	blocked := false
	for !blocked {
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))", owner).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("event did not serialize with settings: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events, err := repo.PendingTelegram(ctx, "concurrent-test")
	if err != nil || len(events) != 0 {
		t.Fatalf("disabled category was queued: %+v %v", events, err)
	}
}
