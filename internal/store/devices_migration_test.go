package store

import (
	"context"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDevicesMigrationPreservesLegacyKeys(t *testing.T) {
	dsn := testdb.URL(t)
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "device_migration_" + strings.ReplaceAll(strings.ToLower(NewToken()[:12]), "-", "_")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	dbURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := dbURL.Query()
	query.Set("search_path", schema)
	dbURL.RawQuery = query.Encode()
	pool, err := pgxpool.New(ctx, dbURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbURL.Scheme = "pgx5"
	src, err := iofs.New(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := migrate.NewWithSourceInstance("iofs", src, dbURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer migration.Close()
	if err := migration.Migrate(21); err != nil {
		t.Fatal(err)
	}
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := New(pool, vault)
	var userID, subID string
	if err := pool.QueryRow(ctx, `INSERT INTO users(username) VALUES('legacy-user') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	token := NewToken()
	if err := pool.QueryRow(ctx, `INSERT INTO subscriptions(user_id,name,token_hash,expires_at) VALUES($1,'Legacy',$2,$3) RETURNING id::text`, userID, HashToken(token), time.Now().Add(time.Hour)).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	private, public, err := vpn.KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	_, serverPublic, err := vpn.KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	header, _, err := vpn.KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	node, err := repo.ReserveNode(ctx, middleware.GetReqID(ctx), "Legacy node", "192.0.2.70", NewToken(), 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE nodes SET status='ready',public_key=$2,header_key=$3,applied_revision=revision,last_seen_at=now() WHERE id=$1`, node.ID, serverPublic, vault.Seal(header)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO peers(subscription_id,node_id,address,private_key,public_key) VALUES($1,$2,2,$3,$4)`, subID, node.ID, vault.Seal(private), public); err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(); err != nil {
		t.Fatal(err)
	}
	profile, err := repo.Profile(ctx, middleware.GetReqID(ctx), token, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.PrivateKey != private || profile.PublicKey != serverPublic || profile.HeaderKey != header || profile.Address != "10.70.0.2/32" {
		t.Fatal("migration changed existing VPN credentials")
	}
	devices, err := repo.Devices(ctx, middleware.GetReqID(ctx), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Name != "Main device" {
		t.Fatal("legacy keys were not assigned to one device")
	}
	user, err := repo.User(ctx, middleware.GetReqID(ctx), userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.DeviceLimit != nil || user.DeviceCount != 1 {
		t.Fatal("migration unexpectedly restricted an existing user")
	}
	if err := migration.Steps(-1); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	var address int
	if err := pool.QueryRow(ctx, `SELECT private_key,address FROM peers WHERE subscription_id=$1 AND node_id=$2`, subID, node.ID).Scan(&encrypted, &address); err != nil {
		t.Fatal(err)
	}
	restored, err := vault.Open(encrypted)
	if err != nil || restored != private || address != 2 {
		t.Fatal("rollback changed legacy VPN credentials")
	}
	if err := migration.Up(); err != nil {
		t.Fatal(err)
	}
}
