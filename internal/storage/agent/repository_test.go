package agent_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestAgentAuthenticationRejectsRetiredNode(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	node, err := repo.ReserveNode(ctx, requestID, "Netherlands", "203.0.113.7", "agent-test", 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishInstall(ctx, requestID, node.ID, "agent-test", "public-key", "header-key", ""); err != nil {
		t.Fatal(err)
	}
	ok, err := repo.AgentExists(ctx, requestID, node.ID, "agent-test")
	if err != nil || !ok {
		t.Fatal("ready agent rejected", err)
	}
	if err := repo.ForgetNode(ctx, requestID, node.ID); err != nil {
		t.Fatal(err)
	}
	ok, err = repo.AgentExists(ctx, requestID, node.ID, "agent-test")
	if err != nil || ok {
		t.Fatal("retired agent accepted", err)
	}
}
