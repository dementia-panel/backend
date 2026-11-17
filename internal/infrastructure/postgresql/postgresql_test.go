package postgresql

import (
	"dementia/backend/internal/infrastructure/config"
	"dementia/backend/internal/testdb"
	"testing"
)

func TestOwnershipLockIsExclusiveAndReleased(t *testing.T) {
	c := &config.Config{DatabaseURL: testdb.URL(t)}
	first, err := New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.Context(), c)
	if err == nil {
		second.Close()
		first.Close()
		t.Fatal("second backend acquired ownership")
	}
	first.Close()
	next, err := New(t.Context(), c)
	if err != nil {
		t.Fatal("ownership lock was not released", err)
	}
	next.Close()
}
