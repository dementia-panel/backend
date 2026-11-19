package plans_test

import (
	"context"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"errors"
	"fmt"
	"testing"
	"time"
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
	user, err := repo.CreateUser(ctx, requestID, "alice", nil, nil)
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

func TestPlanNodeChangesPreserveKeysAndBumpOnce(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "bulk-plan-test"
	nodeIDs := make([]string, 2)
	for i := range nodeIDs {
		node, err := repo.ReserveNode(ctx, requestID, fmt.Sprintf("Node %d", i), fmt.Sprintf("203.0.113.%d", i+10), fmt.Sprintf("token-%d", i), 22, 51820+i)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.FinishInstall(ctx, requestID, node.ID, fmt.Sprintf("token-%d", i), fmt.Sprintf("public-%d", i), fmt.Sprintf("header-%d", i), ""); err != nil {
			t.Fatal(err)
		}
		nodeIDs[i] = node.ID
	}
	plan, err := repo.SaveSubscriptionPlan(ctx, requestID, "", "Europe", nodeIDs[:1])
	if err != nil {
		t.Fatal(err)
	}
	for i := range 80 {
		user, err := repo.CreateUser(ctx, requestID, fmt.Sprintf("subscriber-%d", i), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.AssignSubscriptionPlan(ctx, requestID, user.ID, &plan.ID, fmt.Sprintf("subscription-token-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var originalKey string
	var originalRevision, addedRevision int64
	if err := pool.QueryRow(ctx, `SELECT public_key FROM peers WHERE node_id=$1 ORDER BY subscription_id LIMIT 1`, nodeIDs[0]).Scan(&originalKey); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[0]).Scan(&originalRevision); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[1]).Scan(&addedRevision); err != nil {
		t.Fatal(err)
	}
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repo.SaveSubscriptionPlan(updateCtx, requestID, plan.ID, "Europe", nodeIDs[:2]); err != nil {
		t.Fatal(err)
	}
	var count int
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM peers WHERE node_id=$1`, nodeIDs[1]).Scan(&count); err != nil || count != 80 {
		t.Fatalf("new node peers: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[1]).Scan(&revision); err != nil || revision != addedRevision+1 {
		t.Fatalf("new node revision: got=%d want=%d err=%v", revision, addedRevision+1, err)
	}
	if _, err := repo.SaveSubscriptionPlan(ctx, requestID, plan.ID, "Europe", nodeIDs[:2]); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[1]).Scan(&revision); err != nil || revision != addedRevision+1 {
		t.Fatalf("no-op revision: got=%d want=%d err=%v", revision, addedRevision+1, err)
	}
	unavailable, err := repo.ReserveNode(ctx, requestID, "Unavailable", "203.0.113.30", "unavailable-token", 22, 51822)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveSubscriptionPlan(ctx, requestID, plan.ID, "Europe", []string{nodeIDs[0], unavailable.ID}); !errors.Is(err, store.ErrNodeUnavailable) {
		t.Fatalf("unavailable node should roll back: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM subscription_plan_nodes WHERE plan_id=$1`, plan.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed update changed plan nodes: count=%d err=%v", count, err)
	}
	var preservedKey string
	if err := pool.QueryRow(ctx, `SELECT public_key FROM peers WHERE node_id=$1 ORDER BY subscription_id LIMIT 1`, nodeIDs[0]).Scan(&preservedKey); err != nil || preservedKey != originalKey {
		t.Fatalf("unchanged key was replaced: err=%v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[0]).Scan(&revision); err != nil || revision != originalRevision {
		t.Fatalf("unchanged node revision: got=%d want=%d err=%v", revision, originalRevision, err)
	}
	if _, err := repo.SaveSubscriptionPlan(ctx, requestID, plan.ID, "Europe", nodeIDs[1:2]); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM peers WHERE node_id=$1`, nodeIDs[0]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("removed node peers: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM nodes WHERE id=$1`, nodeIDs[0]).Scan(&revision); err != nil || revision != originalRevision+1 {
		t.Fatalf("removed node revision: got=%d want=%d err=%v", revision, originalRevision+1, err)
	}
}
