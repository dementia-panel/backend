package api_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dementia/backend/internal/api"
	"dementia/backend/internal/store"
	"dementia/backend/internal/vpn"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubscriptionAcrossNodes(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "webhook_disabled"
		if enabled {
			name = "webhook_enabled"
		}
		t.Run(name, func(t *testing.T) { testSubscriptionAcrossNodes(t, enabled) })
	}
}

func testSubscriptionAcrossNodes(t *testing.T, webhookEnabled bool) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is unset")
	}
	ctx := t.Context()
	dbURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(strings.ToLower(store.NewToken()[:12]), "-", "_")
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	query := dbURL.Query()
	query.Set("search_path", schema)
	dbURL.RawQuery = query.Encode()
	config, err := pgxpool.ParseConfig(dbURL.String())
	if err != nil {
		t.Fatal(err)
	}
	if webhookEnabled {
		config.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "on"
	}
	config.ConnConfig.RuntimeParams["dementia.telegram_enabled"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbURL.Scheme = "pgx5"
	if err := store.Migrate(dbURL.String()); err != nil {
		t.Fatal(err)
	}
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.Store{DB: pool, Vault: vault}
	var admin, refresh string
	signingKey := bytes.Repeat([]byte{9}, 32)
	const origin = "https://example.test"
	handler := (&api.API{Store: repo, JWTSigningKey: signingKey, PublicURL: origin, Context: ctx}).Handler()

	call := func(method, path, token string, payload any, status int, target any) string {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if admin != "" && token == admin {
			req.AddCookie(&http.Cookie{Name: "__Host-dementia-access", Value: admin})
			if refresh != "" {
				req.AddCookie(&http.Cookie{Name: "__Host-dementia-refresh", Value: refresh})
			}
		} else if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s returned %d, expected %d: %s", method, path, res.Code, status, res.Body)
		}
		for _, cookie := range res.Result().Cookies() {
			if cookie.MaxAge <= 0 {
				continue
			}
			if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal("session cookie lacks security attributes")
			}
			switch cookie.Name {
			case "__Host-dementia-access":
				admin = cookie.Value
			case "__Host-dementia-refresh":
				refresh = cookie.Value
			}
		}
		if target != nil {
			if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
		return res.Body.String()
	}
	var setup map[string]bool
	call(http.MethodGet, "/api/auth/setup", "", nil, 200, &setup)
	if !setup["setup_required"] || !setup["setup_available"] {
		t.Fatal("setup should be available on an empty database")
	}
	call(http.MethodGet, "/api/admin/users", "", nil, 401, nil)
	call(http.MethodGet, "/api/admin/settings/integrations", "", nil, 401, nil)
	creds := map[string]string{"username": "owner", "password": "a-test-password-long-enough"}
	call(http.MethodPost, "/api/auth/setup", "", map[string]string{"username": "owner", "password": "short"}, 400, nil)
	call(http.MethodPost, "/api/auth/setup", "", creds, 201, nil)
	call(http.MethodPost, "/api/auth/setup", "", creds, 409, nil)

	var integrations struct {
		Telegram struct {
			State string `json:"state"`
		}
		Webhook struct {
			State string `json:"state"`
		}
	}
	call(http.MethodGet, "/api/admin/settings/integrations", admin, nil, 200, &integrations)
	if integrations.Telegram.State != "disabled" || integrations.Webhook.State != "disabled" {
		t.Fatal("missing integration defaults")
	}
	// Recreate the handler to simulate restarting the backend.
	handler = (&api.API{Store: repo, JWTSigningKey: signingKey, PublicURL: origin, Context: ctx}).Handler()
	call(http.MethodGet, "/api/auth/me", admin, nil, 200, nil)
	call(http.MethodPost, "/api/auth/logout", admin, nil, 204, nil)
	call(http.MethodGet, "/api/auth/me", admin, nil, 401, nil)
	call(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "owner", "password": "wrong"}, 401, nil)
	call(http.MethodPost, "/api/auth/login", "", creds, 200, nil)
	call(http.MethodGet, "/api/admin/users", admin, nil, 200, nil)

	claims := new(jwt.RegisteredClaims)
	if _, err := jwt.ParseWithClaims(admin, claims, func(*jwt.Token) (any, error) { return signingKey, nil }); err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) != 15*time.Minute {
		t.Fatal("wrong access lifetime")
	}
	refreshClaims := new(jwt.RegisteredClaims)
	if _, err := jwt.ParseWithClaims(refresh, refreshClaims, func(*jwt.Token) (any, error) { return signingKey, nil }); err != nil {
		t.Fatal(err)
	}
	if refreshClaims.ExpiresAt.Sub(refreshClaims.IssuedAt.Time) != 4*time.Hour {
		t.Fatal("wrong refresh lifetime")
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-16 * time.Minute))
	claims.NotBefore = claims.IssuedAt
	expiredAccess, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingKey)
	if err != nil {
		t.Fatal(err)
	}
	admin = expiredAccess
	oldRefresh := refresh
	call(http.MethodGet, "/api/admin/users", admin, nil, 200, nil)
	if admin == expiredAccess || refresh == oldRefresh || refresh == "" {
		t.Fatal("middleware did not rotate both tokens")
	}
	newAccess, newRefresh := admin, refresh
	admin, refresh = expiredAccess, oldRefresh
	call(http.MethodGet, "/api/admin/users", admin, nil, 409, nil)
	admin, refresh = newAccess, newRefresh
	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-dementia-refresh", Value: refresh})
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != 200 || len(result.Result().Cookies()) != 2 {
		t.Fatal("middleware did not refresh when browser omitted expired access cookie")
	}
	for _, cookie := range result.Result().Cookies() {
		switch cookie.Name {
		case "__Host-dementia-access":
			admin = cookie.Value
		case "__Host-dementia-refresh":
			refresh = cookie.Value
		}
	}

	const tokenPath = "/api/admin/api-tokens"
	call(http.MethodPost, tokenPath, "", map[string]string{"name": "bot"}, 401, nil)
	call(http.MethodPost, tokenPath, admin, map[string]string{"name": " "}, 400, nil)
	var serviceToken struct {
		store.APIToken
		Token string `json:"token"`
	}
	call(http.MethodPost, tokenPath, admin, map[string]string{"name": " Telegram bot "}, 201, &serviceToken)
	if serviceToken.Name != "Telegram bot" || serviceToken.ID == "" || !strings.HasPrefix(serviceToken.Token, "dementia_api_") {
		t.Fatalf("unexpected API token metadata: %+v", serviceToken.APIToken)
	}
	var tokens []store.APIToken
	rawList := call(http.MethodGet, tokenPath, admin, nil, 200, &tokens)
	if len(tokens) != 1 || tokens[0].ID != serviceToken.ID || strings.Contains(rawList, serviceToken.Token) {
		t.Fatal("API token list did not return only metadata")
	}
	call(http.MethodGet, "/api/admin/users", "amz_api_"+store.NewToken(), nil, 401, nil)
	call(http.MethodGet, "/api/admin/users", serviceToken.Token, nil, 200, nil)
	legacyKey := "amz_api_" + store.NewToken()
	legacy, err := repo.CreateAPIToken(ctx, "Legacy integration", legacyKey)
	if err != nil {
		t.Fatal(err)
	}
	call(http.MethodGet, "/api/admin/users", legacyKey, nil, 200, nil)
	if err := repo.DeleteAPIToken(ctx, legacy.ID); err != nil {
		t.Fatal(err)
	}
	call(http.MethodGet, "/api/admin/users", legacyKey, nil, 401, nil)

	call(http.MethodGet, tokenPath, serviceToken.Token, nil, 401, nil)
	call(http.MethodDelete, tokenPath+"/"+serviceToken.ID, serviceToken.Token, nil, 401, nil)
	handler = (&api.API{Store: repo, JWTSigningKey: signingKey, PublicURL: origin, Context: ctx}).Handler()
	call(http.MethodGet, "/api/admin/users", serviceToken.Token, nil, 200, nil)
	call(http.MethodDelete, tokenPath+"/"+serviceToken.ID, admin, nil, 204, nil)
	call(http.MethodGet, "/api/admin/users", serviceToken.Token, nil, 401, nil)
	call(http.MethodDelete, tokenPath+"/"+serviceToken.ID, admin, nil, 404, nil)

	type fixture struct {
		id, token, public, header string
	}
	var nodes []fixture
	for i, ip := range []string{"192.0.2.101", "192.0.2.102"} {
		token := store.NewToken()
		node, err := repo.ReserveNode(ctx, ip, ip, token, 22, 51820+i)
		if err != nil {
			t.Fatal(err)
		}
		header, public, err := vpn.KeyPair()
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.TrustNodeHostKey(ctx, node.ID, "invalid-agent-token", "first-host-key"); err == nil {
			t.Fatal("stale installation could pin a server key")
		}
		if err := repo.TrustNodeHostKey(ctx, node.ID, token, "first-host-key"); err != nil {
			t.Fatal(err)
		}
		if err := repo.TrustNodeHostKey(ctx, node.ID, token, "first-host-key"); err != nil {
			t.Fatal(err)
		}
		if err := repo.TrustNodeHostKey(ctx, node.ID, token, "changed-host-key"); err == nil {
			t.Fatal("a changed server key was accepted")
		}
		if err := repo.FinishInstall(ctx, node.ID, token, public, header, ""); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, fixture{node.ID, token, public, header})
	}
	var user store.User
	call(http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "Тест", "telegram_id": 123456789}, 201, &user)
	var sub struct {
		store.Subscription
		URL string
	}
	call(http.MethodPost, "/api/admin/subscriptions", admin, map[string]any{
		"user_id": user.ID, "name": "<Две ноды>", "node_ids": []string{nodes[1].id, nodes[0].id},
		"expires_at": time.Now().Add(time.Hour),
	}, 201, &sub)
	path := strings.TrimPrefix(sub.URL, origin)
	if html := call(http.MethodGet, path, "", nil, 200, nil); !strings.Contains(html, "&lt;Две ноды&gt;") {
		t.Fatal("subscription page did not escape its title")
	}

	var privateKeys []string
	for _, node := range nodes {
		profile := path + "/nodes/" + node.id + ".conf"
		adminProfile := "/api/admin/subscriptions/" + sub.ID + "/nodes/" + node.id + "/profile"
		call(http.MethodGet, adminProfile, "", nil, 401, nil)
		var issued struct{ Conf, Key string }
		call(http.MethodGet, adminProfile, admin, nil, 200, &issued)
		call(http.MethodGet, profile, "", nil, 404, nil)
		call(http.MethodGet, strings.TrimSuffix(profile, ".conf")+".key", "", nil, 404, nil)
		agent := "/api/agent/nodes/" + node.id
		var state store.State
		call(http.MethodGet, agent+"/state", node.token, nil, 200, &state)
		if len(state.Peers) != 1 {
			t.Fatalf("expected one peer, got %d", len(state.Peers))
		}
		call(http.MethodPost, agent+"/ack", node.token, map[string]int64{"revision": state.Revision}, 200, nil)
		conf := call(http.MethodGet, profile, "", nil, 200, nil)
		if conf != issued.Conf || !strings.HasPrefix(issued.Key, "vpn://") {
			t.Fatal("administrator export does not match the previously issued profile")
		}
		if !strings.Contains(conf, "HeaderProtectionKey = "+node.header) || !strings.Contains(conf, "PublicKey = "+node.public) {
			t.Fatal("profile does not match its node")
		}
		privateKeys = append(privateKeys, strings.Split(conf, "\n")[1])
	}
	if privateKeys[0] == privateKeys[1] {
		t.Fatal("node profiles share a private key")
	}
	html := call(http.MethodGet, path, "", nil, 200, nil)
	var manifest struct {
		Locations []struct {
			ID     string `json:"id"`
			KeyURL string `json:"key_url"`
		} `json:"locations"`
	}
	call(http.MethodGet, path+"/manifest", "", nil, 200, &manifest)
	if len(manifest.Locations) != len(nodes) {
		t.Fatal("manifest does not include all nodes")
	}
	keys := make(map[string]string, len(nodes))
	for _, location := range manifest.Locations {
		profileURL := path + "/nodes/" + location.ID
		if location.KeyURL != origin+profileURL+".key" || !strings.Contains(html, location.KeyURL) {
			t.Fatal("missing text key link for node")
		}
		key := call(http.MethodGet, profileURL+".key", "", nil, 200, nil)
		if !strings.HasPrefix(key, "vpn://") || strings.Contains(html, key) {
			t.Fatal("text key is invalid or was embedded in the page")
		}
		if file := call(http.MethodGet, profileURL+".vpn", "", nil, 200, nil); key != file {
			t.Fatal("text and file formats produced different keys")
		}
		keys[location.ID] = key
	}
	if keys[nodes[0].id] == keys[nodes[1].id] {
		t.Fatal("different nodes have the same text key")
	}
	nodePath := "/api/admin/nodes/" + nodes[0].id
	agentPath := "/api/agent/nodes/" + nodes[0].id
	const newName = "Финляндия 2"
	var renamed store.Node
	call(http.MethodPatch, nodePath, admin, map[string]any{"name": newName}, 200, &renamed)
	call(http.MethodPatch, nodePath, admin, map[string]any{"name": "invalid\nname"}, 400, nil)
	var nodeState store.State
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if nodeState.Name != newName || len(nodeState.Peers) != 1 {
		t.Fatalf("rename changed the peer assignment or did not reach agent: %+v", nodeState)
	}
	profile := path + "/nodes/" + nodes[0].id
	if conf := call(http.MethodGet, profile+".conf", "", nil, 200, nil); !strings.Contains(conf, privateKeys[0]) {
		t.Fatal("rename changed the client key")
	}
	link := call(http.MethodGet, profile+".vpn", "", nil, 200, nil)
	if !strings.HasPrefix(link, "vpn://") {
		t.Fatal("missing AmneziaVPN key prefix")
	}
	compressed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, "vpn://"))
	if err != nil || len(compressed) < 4 {
		t.Fatal("invalid AmneziaVPN encoding")
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed[4:]))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || len(raw) != int(binary.BigEndian.Uint32(compressed[:4])) {
		t.Fatal("invalid AmneziaVPN compression")
	}
	var guest struct {
		Description string `json:"description"`
		Containers  []struct {
			AWG struct {
				LastConfig string `json:"last_config"`
			} `json:"awg"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(raw, &guest); err != nil || guest.Description != newName || len(guest.Containers) != 1 {
		t.Fatal("AmneziaVPN guest profile has wrong name or protocol")
	}
	var client map[string]any
	if err := json.Unmarshal([]byte(guest.Containers[0].AWG.LastConfig), &client); err != nil || client["HeaderProtectionKey"] != nodes[0].header {
		t.Fatal("AmneziaWG 3.1 parameters missing from guest profile")
	}

	call(http.MethodPatch, nodePath, admin, map[string]any{"enabled": false}, 200, nil)
	call(http.MethodGet, profile+".vpn", "", nil, 404, nil)
	call(http.MethodGet, profile+".key", "", nil, 404, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 0 {
		t.Fatal("paused node still receives peers")
	}
	call(http.MethodPost, agentPath+"/ack", nodes[0].token, map[string]int64{"revision": nodeState.Revision}, 200, nil)
	call(http.MethodPatch, nodePath, admin, map[string]any{"enabled": true}, 200, nil)
	call(http.MethodGet, profile+".conf", "", nil, 404, nil)
	var restart store.Node
	call(http.MethodPost, nodePath+"/actions/restart", admin, nil, 202, &restart)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if nodeState.RestartRevision != restart.RestartRevision || nodeState.RestartRevision != 1 || len(nodeState.Peers) != 1 {
		t.Fatalf("unexpected resumed state: %+v", nodeState)
	}
	call(http.MethodPost, agentPath+"/ack", nodes[0].token, map[string]int64{"revision": nodeState.Revision, "restarted_revision": nodeState.RestartRevision}, 200, nil)
	call(http.MethodGet, nodePath, admin, nil, 200, &restart)
	if !restart.Online || restart.RestartedRevision != 1 {
		t.Fatalf("restart not confirmed: %+v", restart)
	}
	if conf := call(http.MethodGet, profile+".conf", "", nil, 200, nil); !strings.Contains(conf, privateKeys[0]) {
		t.Fatal("pause and resume changed the client key")
	}

	userPath := "/api/admin/users/" + user.ID
	if user.Username != "Тест" || user.TelegramID == nil || *user.TelegramID != 123456789 || user.UpdatedAt.IsZero() {
		t.Fatalf("unexpected user fields: %+v", user)
	}
	call(http.MethodPatch, userPath, admin, map[string]any{"username": "updated", "telegram_id": nil}, 204, nil)
	var updated store.User
	call(http.MethodGet, userPath, admin, nil, 200, &updated)
	if updated.Username != "updated" || updated.TelegramID != nil || !updated.UpdatedAt.After(user.UpdatedAt) || updated.Online {
		t.Fatalf("unexpected updated user: %+v", updated)
	}
	secondAgent := "/api/agent/nodes/" + nodes[1].id
	var secondState store.State
	call(http.MethodGet, secondAgent+"/state", nodes[1].token, nil, 200, &secondState)
	handshake := time.Now().Truncate(time.Second)
	call(http.MethodPost, secondAgent+"/ack", nodes[1].token, map[string]any{
		"revision": secondState.Revision, "handshakes": map[string]int64{secondState.Peers[0].PublicKey: handshake.Unix()},
	}, 200, nil)
	call(http.MethodGet, userPath, admin, nil, 200, &updated)
	if !updated.Online || updated.LastConnectedAt == nil || !updated.LastConnectedAt.Equal(handshake) {
		t.Fatalf("missing VPN activity: %+v", updated)
	}
	call(http.MethodPost, secondAgent+"/ack", nodes[1].token, map[string]any{
		"revision": secondState.Revision, "handshakes": map[string]int64{},
	}, 200, nil)
	call(http.MethodGet, userPath, admin, nil, 200, &updated)
	if updated.Online || updated.LastConnectedAt == nil || !updated.LastConnectedAt.Equal(handshake) {
		t.Fatalf("empty report did not clear online while preserving history: %+v", updated)
	}

	adminPath := "/api/admin/subscriptions/" + sub.ID
	call(http.MethodPut, adminPath+"/nodes", admin, map[string]any{"node_ids": []string{nodes[0].id}}, 204, nil)
	call(http.MethodGet, path+"/nodes/"+nodes[1].id+".conf", "", nil, 404, nil)
	var removed store.State
	call(http.MethodGet, "/api/agent/nodes/"+nodes[1].id+"/state", nodes[1].token, nil, 200, &removed)
	if len(removed.Peers) != 0 {
		t.Fatal("removed node still has an assigned peer")
	}
	call(http.MethodGet, userPath, admin, nil, 200, &updated)
	if updated.LastConnectedAt == nil || !updated.LastConnectedAt.Equal(handshake) {
		t.Fatalf("removing a node erased connection history: %+v", updated)
	}

	var rotated struct{ URL string }
	call(http.MethodPost, adminPath+"/rotate-link", admin, nil, 200, &rotated)
	call(http.MethodGet, path, "", nil, 404, nil)
	path = strings.TrimPrefix(rotated.URL, origin)
	call(http.MethodGet, path+"/manifest", "", nil, 200, nil)
	call(http.MethodPatch, adminPath, admin, map[string]any{"enabled": false}, 204, nil)
	call(http.MethodGet, path+"/manifest", "", nil, 404, nil)
	var revoked store.State
	call(http.MethodGet, "/api/agent/nodes/"+nodes[0].id+"/state", nodes[0].token, nil, 200, &revoked)
	if len(revoked.Peers) != 0 {
		t.Fatal("disabled subscription still has an active peer")
	}

	call(http.MethodPut, nodePath+"/endpoint", admin, map[string]any{"host": "nl.example.test", "port": 12345}, 200, nil)

	call(http.MethodPost, "/api/admin/users/bulk/enabled", admin, map[string]any{"ids": []string{user.ID}, "enabled": false}, 200, nil)
	call(http.MethodPost, "/api/admin/users/bulk/enabled", admin, map[string]any{"ids": []string{user.ID}, "enabled": true}, 200, nil)
	call(http.MethodPatch, adminPath, admin, map[string]any{"enabled": true}, 204, nil)
	call(http.MethodPut, adminPath+"/traffic-limit", admin, map[string]int64{"traffic_limit_bytes": 100}, 204, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	call(http.MethodPost, agentPath+"/ack", nodes[0].token, map[string]int64{"revision": nodeState.Revision}, 200, nil)
	if conf := call(http.MethodGet, path+"/nodes/"+nodes[0].id+".conf", "", nil, 200, nil); !strings.Contains(conf, "Endpoint = nl.example.test:12345") {
		t.Fatal("profile did not use configured endpoint")
	}

	oldPeer := nodeState.Peers[0].PublicKey
	report := map[string]any{"revision": nodeState.Revision, "traffic": map[string]store.TrafficCounter{
		oldPeer: {Received: 60, Sent: 50},
	}}
	call(http.MethodPost, agentPath+"/ack", nodes[0].token, report, 200, nil)
	call(http.MethodPost, agentPath+"/ack", nodes[0].token, report, 200, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 0 {
		t.Fatal("quota did not remove the peer")
	}
	var usage []store.DailyTraffic
	call(http.MethodGet, "/api/admin/traffic?user_id="+user.ID+"&node_id="+nodes[0].id, admin, nil, 200, &usage)
	if len(usage) != 1 || usage[0].Received != 60 || usage[0].Sent != 50 {
		t.Fatalf("wrong traffic totals: %+v", usage)
	}
	call(http.MethodPut, adminPath+"/traffic-reset-schedule", admin, map[string]int{"traffic_reset_days": 30}, 204, nil)
	if _, err := pool.Exec(ctx, "UPDATE subscriptions SET traffic_reset_at=now() WHERE id=$1", sub.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetDueTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 1 {
		t.Fatal("scheduled reset did not restore access")
	}

	call(http.MethodPost, adminPath+"/reset-traffic", admin, nil, 204, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 1 {
		t.Fatal("reset did not restore the peer")
	}
	call(http.MethodPost, adminPath+"/revoke", admin, nil, 200, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 1 || nodeState.Peers[0].PublicKey == oldPeer {
		t.Fatal("revoke did not replace the VPN key")
	}

	checkConnection := func(ack bool, expected int) {
		t.Helper()
		check, err := repo.StartConnectionCheck(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if ack {
			for _, node := range nodes {
				var state store.State
				agent := "/api/agent/nodes/" + node.id
				call(http.MethodGet, agent+"/state", node.token, nil, 200, &state)
				call(http.MethodPost, agent+"/ack", node.token, map[string]int64{"revision": state.Revision}, 200, nil)
			}
		}
		changes, err := repo.FinishConnectionCheck(ctx, check)
		if err != nil || len(changes) != expected {
			t.Fatalf("connection transitions: %+v, error: %v", changes, err)
		}
		for _, change := range changes {
			if change.Online != ack {
				t.Fatalf("wrong connection status: %+v", change)
			}
		}
	}
	checkConnection(true, 0)
	checkConnection(false, 2)
	checkConnection(false, 0)
	checkConnection(true, 2)
	checkConnection(true, 0)

	call(http.MethodPost, "/api/admin/nodes/actions/restart-all", admin, nil, 202, nil)
	call(http.MethodDelete, nodePath, admin, nil, 202, nil)
	call(http.MethodGet, agentPath+"/state", nodes[0].token, nil, 200, &nodeState)
	if len(nodeState.Peers) != 0 {
		t.Fatal("retired node still has peers")
	}
	call(http.MethodDelete, adminPath, admin, nil, 204, nil)
	call(http.MethodDelete, userPath, admin, nil, 204, nil)
	call(http.MethodGet, userPath, admin, nil, 404, nil)

	var telegramEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telegram_notifications
		WHERE event IN ('node.created','node.connection_lost','node.connection_restored','node.deleted')`).Scan(&telegramEvents); err != nil || telegramEvents != 7 {
		t.Fatalf("expected seven node notifications independently of webhook, got %d: %v", telegramEvents, err)
	}

	// First-login onboarding must be authenticated, mandatory, and atomic.
	call(http.MethodPost, "/api/auth/complete-setup", "", creds, 401, nil)
	if _, err := pool.Exec(ctx, "UPDATE administrators SET password_change_required=true"); err != nil {
		t.Fatal(err)
	}
	var me struct {
		Username string `json:"username"`
		Required bool   `json:"password_change_required"`
	}
	call(http.MethodGet, "/api/auth/me", admin, nil, 200, &me)
	if !me.Required {
		t.Fatal("missing mandatory onboarding state")
	}
	call(http.MethodGet, "/api/admin/users", admin, nil, 403, nil)
	call(http.MethodPost, "/api/auth/complete-setup", admin, map[string]string{"username": "owner", "password": "short"}, 400, nil)
	oldAccess := admin
	staleSalt, staleHash, err := repo.AdminCredentials(ctx, creds["username"])
	if err != nil {
		t.Fatal(err)
	}
	permanent := map[string]string{"username": "dementia-owner", "password": "a-permanent-password-long-enough"}
	call(http.MethodPost, "/api/auth/complete-setup", admin, permanent, 200, nil)
	call(http.MethodGet, "/api/auth/me", admin, nil, 200, &me)
	if me.Required || me.Username != "dementia-owner" {
		t.Fatal("onboarding was not persisted")
	}
	call(http.MethodGet, "/api/admin/users", admin, nil, 200, nil)
	oldRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	oldRequest.AddCookie(&http.Cookie{Name: "__Host-dementia-access", Value: oldAccess})
	oldResponse := httptest.NewRecorder()
	handler.ServeHTTP(oldResponse, oldRequest)
	if oldResponse.Code != 401 {
		t.Fatal("temporary session remained valid")
	}

	staleIssueCalled := false
	err = repo.CreateAdminSession(ctx, creds["username"], staleSalt, staleHash, func(string) (string, time.Time, error) { staleIssueCalled = true; return "", time.Time{}, nil })
	if !errors.Is(err, pgx.ErrNoRows) || staleIssueCalled {
		t.Fatal("stale password verification could create a new session")
	}
	call(http.MethodPost, "/api/auth/login", "", creds, 401, nil)
	call(http.MethodPost, "/api/auth/login", "", permanent, 200, nil)
	call(http.MethodPost, "/api/auth/complete-setup", admin, permanent, 409, nil)

	events, err := repo.PendingEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !webhookEnabled {
		if len(events) != 0 {
			t.Fatalf("webhook disabled but queued %d events", len(events))
		}
		return
	}

	counts := make(map[string]int)
	for _, event := range events {
		if group, _, _ := strings.Cut(event.Name, "."); event.Scope != group {
			t.Errorf("%s has wrong scope %q", event.Name, event.Scope)
		}
		counts[event.Name]++
	}
	for name, expected := range map[string]int{
		"user.created": 1, "node.created": 2, "node.modified": 1, "node.connection_lost": 2,
		"node.connection_restored": 2, "subscription.created": 1, "subscription.nodes_changed": 1,
	} {
		if counts[name] != expected {
			t.Errorf("%s: got %d, want %d", name, counts[name], expected)
		}
	}
	if len(events) == 0 {
		t.Fatal("empty webhook queue")
	}
	if err := repo.RetryEvent(ctx, events[0].ID, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkEventDelivered(ctx, events[1].ID); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.PendingEvents(ctx)
	if err != nil || len(pending) < len(events)-2 {
		t.Fatalf("unexpected webhook queue length: %d (%v)", len(pending), err)
	}
	for _, event := range pending {
		if event.ID == events[0].ID || event.ID == events[1].ID {
			t.Fatalf("rescheduled or delivered webhook remained pending: %d", event.ID)
		}
	}

}
