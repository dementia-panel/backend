package nodes_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
	"time"
)

func TestRetryDoesNotAcceptStaleInstaller(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	node, err := repo.ReserveNode(ctx, requestID, "First", "203.0.113.9", "first-token", 22, 51820)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishInstall(ctx, requestID, node.ID, "first-token", "", "", "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE nodes SET connection_lost_at=now()-interval '10 minutes',connection_alerted=true WHERE id=$1", node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReserveNode(ctx, requestID, "Retry", "203.0.113.9", "second-token", 22, 51820); err != nil {
		t.Fatal(err)
	}
	var reset bool
	if err := pool.QueryRow(ctx, "SELECT connection_lost_at IS NULL AND NOT connection_alerted FROM nodes WHERE id=$1", node.ID).Scan(&reset); err != nil || !reset {
		t.Fatalf("node retry retained previous outage: %v", err)
	}
	if err := repo.TrustNodeHostKey(ctx, requestID, node.ID, "first-token", "old-fingerprint"); err == nil {
		t.Fatal("stale installer changed host key")
	}
	if err := repo.TrustNodeHostKey(ctx, requestID, node.ID, "second-token", "current-fingerprint"); err != nil {
		t.Fatal(err)
	}
}

func TestNodeOnlineUserCounts(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, _ := vpn.NewVault(make([]byte, 32))
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "node-devices-test"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var nodes []store.Node
	for _, ip := range []string{"203.0.113.10", "203.0.113.11"} {
		node, err := repo.ReserveNode(ctx, requestID, ip, ip, ip, 22, 51820)
		if err != nil {
			t.Fatal(err)
		}
		private, public, err := vpn.KeyPair()
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.FinishInstall(ctx, requestID, node.ID, ip, public, private, ""); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	user, err := repo.CreateUser(ctx, requestID, "alice", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"First access", "Second access"} {
		if _, err = repo.CreateSubscription(ctx, requestID, user.ID, name, store.NewToken(), time.Now().Add(time.Hour), []string{nodes[0].ID, nodes[1].ID}); err != nil {
			t.Fatal(err)
		}
	}
	device, err := repo.CreateDevice(ctx, requestID, user.ID, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	reset := func() {
		exec("UPDATE users SET enabled=true")
		exec("UPDATE subscriptions SET enabled=true,expires_at=now()+interval '1 hour',traffic_limit_bytes=NULL,traffic_used_bytes=0")
		exec("UPDATE peers SET current_handshake_at=now()")
		exec("UPDATE nodes SET enabled=true,status='ready',applied_revision=revision,handshakes_reported_at=now(),last_seen_at=now()")
	}
	check := func(first, second int) {
		t.Helper()
		all, err := repo.Nodes(ctx, requestID)
		if err != nil || len(all) != 2 {
			t.Fatalf("list nodes: %v", err)
		}
		expected := map[string]int{nodes[0].ID: first, nodes[1].ID: second}
		for _, node := range all {
			if node.OnlineUserCount != expected[node.ID] {
				t.Fatalf("node %s: %d users, want %d", node.ID, node.OnlineUserCount, expected[node.ID])
			}
			detail, err := repo.Node(ctx, requestID, node.ID)
			if err != nil || detail.OnlineUserCount != expected[node.ID] {
				t.Fatalf("node detail: %+v, %v", detail, err)
			}
		}
	}
	reset()
	check(1, 1)
	exec("UPDATE peers SET current_handshake_at=now()-interval '4 minutes' WHERE node_id=$1 AND device_id=$2", nodes[0].ID, device.ID)
	check(1, 1)
	for _, sql := range []string{
		"UPDATE nodes SET handshakes_reported_at=now()-interval '2 minutes' WHERE id=$1",
		"UPDATE nodes SET applied_revision=revision-1 WHERE id=$1",
		"UPDATE nodes SET enabled=false WHERE id=$1",
		"UPDATE nodes SET status='failed' WHERE id=$1",
	} {
		reset()
		exec(sql, nodes[0].ID)
		check(0, 1)
	}
	for _, sql := range []string{
		"UPDATE users SET enabled=false",
		"UPDATE subscriptions SET enabled=false",
		"UPDATE subscriptions SET expires_at=now()-interval '1 minute'",
		"UPDATE subscriptions SET traffic_limit_bytes=1,traffic_used_bytes=1",
	} {
		reset()
		exec(sql)
		check(0, 0)
	}
	reset()
	name := "Renamed"
	renamed, err := repo.UpdateNode(ctx, requestID, nodes[0].ID, &name, nil)
	if err != nil || renamed.OnlineUserCount != 1 {
		t.Fatalf("update returning: %+v %v", renamed, err)
	}
	if err := repo.DeleteDevice(ctx, requestID, user.ID, device.ID); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE nodes SET applied_revision=revision,handshakes_reported_at=now()")
	check(1, 1)
	metrics, err := repo.NodeMetrics(ctx, requestID)
	if err != nil || metrics.OnlineUsers != 1 || metrics.PeakOnlineUsers != 1 {
		t.Fatalf("current unique users: %+v, %v", metrics, err)
	}
	exec(`INSERT INTO traffic_daily(day,node_id,user_id,received_bytes,sent_bytes)
		VALUES ((now() AT TIME ZONE 'UTC')::date,$1,$3,100,25),
		       ((now() AT TIME ZONE 'UTC')::date,$2,$3,200,50)`, nodes[0].ID, nodes[1].ID, user.ID)
	if err := repo.SampleOnlinePeak(ctx, requestID); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE peers SET current_handshake_at=now()-interval '4 minutes'")
	metrics, err = repo.NodeMetrics(ctx, requestID)
	if err != nil || metrics.OnlineUsers != 0 || metrics.PeakOnlineUsers != 1 || metrics.TrafficTodayBytes != 375 {
		t.Fatalf("persisted peak and daily traffic: %+v, %v", metrics, err)
	}
}
