package api_tokens_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestTokenRenameAndRevocation(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	token, err := repo.CreateAPIToken(ctx, requestID, "service", "opaque-token")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := repo.RenameAPIToken(ctx, requestID, token.ID, "renamed")
	if err != nil || renamed.Name != "renamed" {
		t.Fatal("rename failed", err)
	}
	ok, err := repo.APITokenExists(ctx, requestID, "opaque-token")
	if err != nil || !ok {
		t.Fatal("rename changed token", err)
	}
	if err := repo.DeleteAPIToken(ctx, requestID, token.ID); err != nil {
		t.Fatal(err)
	}
	ok, err = repo.APITokenExists(ctx, requestID, "opaque-token")
	if err != nil || ok {
		t.Fatal("deleted token accepted", err)
	}
}
