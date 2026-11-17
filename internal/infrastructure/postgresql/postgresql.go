package postgresql

import (
	"context"
	"dementia/backend/internal/infrastructure/config"
	"dementia/backend/internal/store"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	Pool      *pgxpool.Pool
	Ownership *pgx.Conn
}

func closeLock(lock *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = lock.Close(ctx)
}
func (d *Database) Close() {
	d.Pool.Close()
	closeLock(d.Ownership)
}
func New(ctx context.Context, c *config.Config) (*Database, error) {
	dbConfig, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return nil, errors.New("could not parse PostgreSQL configuration")
	}
	if c.WebhookURL != "" {
		dbConfig.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "on"
	} else {
		dbConfig.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "off"
	}
	if c.Bot.Token != "" {
		dbConfig.ConnConfig.RuntimeParams["dementia.telegram_enabled"] = "on"
	}
	dbConfig.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	dbConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return nil, errors.New("could not initialize PostgreSQL pool")
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lock, err := pgx.Connect(startup, c.DatabaseURL)
	if err != nil {
		return nil, errors.New("could not connect to PostgreSQL")
	}
	defer func() {
		if !ok {
			closeLock(lock)
		}
	}()
	var locked bool
	if err := lock.QueryRow(startup, "SELECT pg_try_advisory_lock(4134971021)").Scan(&locked); err != nil || !locked {
		return nil, errors.New("another panel instance holds the PostgreSQL process lock")
	}
	dbURL, _ := url.Parse(c.DatabaseURL)
	dbURL.Scheme = "pgx5"
	query := dbURL.Query()
	query.Set("connect_timeout", "5")
	query.Set("statement_timeout", "60000")
	query.Set("lock_timeout", "5000")
	dbURL.RawQuery = query.Encode()
	if err := store.Migrate(dbURL.String()); err != nil {
		return nil, fmt.Errorf("could not apply PostgreSQL migrations (%T)", err)
	}
	ok = true
	return &Database{Pool: db, Ownership: lock}, nil
}
