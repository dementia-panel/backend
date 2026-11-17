package lifecycle_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestFailedNodeCanBeForgottenAndReadded(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	node, err := repo.ReserveNode(ctx, requestID, "Failed", "203.0.113.8", "first-token", 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishInstall(ctx, requestID, node.ID, "first-token", "", "", "installation failed"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ForgetNode(ctx, requestID, node.ID); err != nil {
		t.Fatal(err)
	}
	again, err := repo.ReserveNode(ctx, requestID, "Retry", "203.0.113.8", "second-token", 22, 51820)
	if err != nil || again.ID != node.ID {
		t.Fatal("cannot readd forgotten node", err)
	}
}
