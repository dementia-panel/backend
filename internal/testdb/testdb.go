package testdb

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func URL(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18.4", postgres.WithDatabase("dementia"), postgres.WithUsername("dementia"), postgres.WithPassword("integration-test-password"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("start PostgreSQL testcontainer: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := container.Terminate(ctx); err != nil {
			t.Errorf("stop PostgreSQL testcontainer: %v", err)
		}
	})
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return url
}
func Pool(t *testing.T, migrate func(string) error) *pgxpool.Pool {
	t.Helper()
	dsn := URL(t)
	u, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "on"
	u.ConnConfig.RuntimeParams["dementia.telegram_enabled"] = "on"
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Scheme = "pgx5"
	migrationURL := parsed.String()
	if err := migrate(migrationURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
