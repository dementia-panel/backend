package devices_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"errors"
	"testing"
)

func TestDefaultLimitAndDeletedDeviceCapacity(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	limit := 1
	if err := repo.SetDefaultDeviceLimit(ctx, requestID, &limit); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateUser(ctx, requestID, "alice", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.CreateDevice(ctx, requestID, user.ID, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateDevice(ctx, requestID, user.ID, "Second"); !errors.Is(err, store.ErrDeviceLimit) {
		t.Fatal("device limit not enforced", err)
	}
	if err := repo.DeleteDevice(ctx, requestID, user.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateDevice(ctx, requestID, user.ID, "Replacement"); err != nil {
		t.Fatal(err)
	}
}
