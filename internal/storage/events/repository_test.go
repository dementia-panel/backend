package events_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestDeliveredEventsDoNotReturn(t *testing.T) {
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
	events, err := repo.PendingEvents(ctx, requestID)
	if err != nil || len(events) == 0 {
		t.Fatal("missing event", err)
	}
	id := events[0].ID
	if err := repo.MarkEventDelivered(ctx, requestID, id); err != nil {
		t.Fatal(err)
	}
	events, err = repo.PendingEvents(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.ID == id {
			t.Fatal("delivered event returned")
		}
	}
}
