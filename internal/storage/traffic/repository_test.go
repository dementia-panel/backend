package traffic_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
	"time"
)

func TestTrafficLimitAndResetSchedulePersist(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	user, err := repo.CreateUser(ctx, requestID, "alice", nil)
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
	limit := int64(1048576)
	if err := repo.SetTrafficLimit(ctx, requestID, sub.ID, &limit, false); err != nil {
		t.Fatal(err)
	}
	days := 30
	if err := repo.SetTrafficResetSchedule(ctx, requestID, sub.ID, &days); err != nil {
		t.Fatal(err)
	}
	values, err := repo.Subscriptions(ctx, requestID)
	if err != nil || len(values) != 1 || values[0].TrafficLimit == nil || *values[0].TrafficLimit != limit || values[0].TrafficResetDays == nil || *values[0].TrafficResetDays != days {
		t.Fatalf("traffic settings: %+v %v", values, err)
	}
}
