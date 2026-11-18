package subscriptions_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
	"time"
)

func TestRotatingLinkRevokesPreviousURL(t *testing.T) {
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
	node, err := repo.ReserveNode(ctx, requestID, "Netherlands", "203.0.113.7", "agent-test", 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishInstall(ctx, requestID, node.ID, "agent-test", "public-key", "header-key", ""); err != nil {
		t.Fatal(err)
	}
	sub, err := repo.CreateSubscription(ctx, requestID, user.ID, "Europe", "subscription-token", time.Now().Add(time.Hour), []string{node.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RotateToken(ctx, requestID, sub.ID, "replacement-token"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.PublicSubscription(ctx, requestID, "subscription-token"); err == nil {
		t.Fatal("old link accepted")
	}
	if _, _, err := repo.PublicSubscription(ctx, requestID, "replacement-token"); err != nil {
		t.Fatal(err)
	}
}
