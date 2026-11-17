package admin_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
	"time"
)

func TestSessionRotationRevokesPreviousRefresh(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	err = repo.SetupAdmin(ctx, requestID, "owner", []byte("salt"), []byte("hash"), func(id string) (string, time.Time, error) { return "initial-refresh", time.Now().Add(time.Hour), nil })
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := pool.QueryRow(ctx, "SELECT session_id::text FROM admin_sessions").Scan(&id); err != nil {
		t.Fatal(err)
	}
	ok, err := repo.RotateAdminSession(ctx, requestID, id, "initial-refresh", "new-refresh", time.Now().Add(time.Hour))
	if err != nil || !ok {
		t.Fatalf("rotation: %v %v", ok, err)
	}
	ok, err = repo.RotateAdminSession(ctx, requestID, id, "initial-refresh", "reused-refresh", time.Now().Add(time.Hour))
	if err != nil || ok {
		t.Fatal("old refresh accepted")
	}
}
