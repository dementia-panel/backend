package sl

import (
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestErrorAttributeDoesNotExposeDatabaseDetails(t *testing.T) {
	value := Err(&pgconn.PgError{Code: "23505", Message: "private-message", Detail: "private-secret"})
	if value.Key != "error" || !strings.Contains(value.Value.String(), "23505") || strings.Contains(value.Value.String(), "private") {
		t.Fatalf("unexpected error attribute: %v", value)
	}
	if value := Err(nil); value.Key != "error" {
		t.Fatal("nil error attribute invalid")
	}
}
