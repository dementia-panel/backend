package nodes_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestRetryDoesNotAcceptStaleInstaller(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	node, err := repo.ReserveNode(ctx, requestID, "First", "203.0.113.9", "first-token", 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishInstall(ctx, requestID, node.ID, "first-token", "", "", "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReserveNode(ctx, requestID, "Retry", "203.0.113.9", "second-token", 22, 51820); err != nil {
		t.Fatal(err)
	}
	if err := repo.TrustNodeHostKey(ctx, requestID, node.ID, "first-token", "old-fingerprint"); err == nil {
		t.Fatal("stale installer changed host key")
	}
	if err := repo.TrustNodeHostKey(ctx, requestID, node.ID, "second-token", "current-fingerprint"); err != nil {
		t.Fatal(err)
	}
}
