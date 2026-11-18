package users_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestTelegramLookupTracksUpdates(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	user, err := repo.CreateUser(ctx, requestID, "alice", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	telegram := int64(123456789)
	if err := repo.UpdateUser(ctx, requestID, user.ID, nil, &telegram, true, nil); err != nil {
		t.Fatal(err)
	}
	id, err := repo.UserIDByTelegram(ctx, requestID, telegram)
	if err != nil || id != user.ID {
		t.Fatal("Telegram lookup failed", err)
	}
	if err := repo.UpdateUser(ctx, requestID, user.ID, nil, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UserIDByTelegram(ctx, requestID, telegram); err == nil {
		t.Fatal("removed Telegram ID resolved")
	}
}
