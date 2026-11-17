package plans_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"errors"
	"testing"
)

func TestReferencedPlanCannotBeDeleted(t *testing.T) {
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
	user, err := repo.CreateUser(ctx, requestID, "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.SaveSubscriptionPlan(ctx, requestID, "", "Europe", []string{node.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.AssignSubscriptionPlan(ctx, requestID, user.ID, &plan.ID, "plan-token"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSubscriptionPlan(ctx, requestID, plan.ID); !errors.Is(err, store.ErrPlanInUse) {
		t.Fatal("referenced plan deleted", err)
	}
}
