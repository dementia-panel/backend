package telegram_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"strings"
	"testing"
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
