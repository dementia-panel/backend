package api_test

import (
	"bytes"
	"dementia/backend/internal/api"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceKeyRouteBoundary(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	token := "dementia_api_" + store.NewToken()
	issued, err := repo.CreateAPIToken(t.Context(), "test-request", "External service", token)
	if err != nil {
		t.Fatal(err)
	}
	handler := api.NewAPI(repo, make([]byte, 32), "https://panel.example.test", t.Context(), nil).Handler()
	routes := []struct{ method, path string }{
		{"GET", "/api/admin/users"},
		{"POST", "/api/admin/users"},
		{"GET", "/api/admin/users/{id}"},
		{"PATCH", "/api/admin/users/{id}"},
		{"DELETE", "/api/admin/users/{id}"},
		{"POST", "/api/admin/users/bulk/enabled"},
		{"GET", "/api/admin/users/{id}/devices"},
		{"POST", "/api/admin/users/{id}/devices"},
		{"GET", "/api/admin/users/{id}/devices/{deviceID}"},
		{"PATCH", "/api/admin/users/{id}/devices/{deviceID}"},
		{"DELETE", "/api/admin/users/{id}/devices/{deviceID}"},
		{"GET", "/api/admin/users/{id}/devices/{deviceID}/link"},
		{"PUT", "/api/admin/users/{id}/device-limit"},
		{"GET", "/api/admin/users/{id}/devices/{deviceID}/nodes/{nodeID}/profile"},
		{"GET", "/api/admin/nodes"},
		{"GET", "/api/admin/nodes/metrics"},
		{"GET", "/api/admin/nodes/{id}"},
		{"POST", "/api/admin/nodes"},
		{"PATCH", "/api/admin/nodes/{id}"},
		{"PUT", "/api/admin/nodes/{id}/endpoint"},
		{"POST", "/api/admin/nodes/{id}/actions/check"},
		{"POST", "/api/admin/nodes/{id}/actions/restart"},
		{"POST", "/api/admin/nodes/actions/restart-all"},
		{"DELETE", "/api/admin/nodes/{id}"},
		{"GET", "/api/admin/subscription-plans"},
		{"POST", "/api/admin/subscription-plans"},
		{"PUT", "/api/admin/subscription-plans/{id}"},
		{"DELETE", "/api/admin/subscription-plans/{id}"},
		{"GET", "/api/admin/users/{id}/subscription"},
		{"PUT", "/api/admin/users/{id}/subscription"},
		{"GET", "/api/admin/subscriptions"},
		{"POST", "/api/admin/subscriptions"},
		{"GET", "/api/admin/subscriptions/{id}/link"},
		{"PATCH", "/api/admin/subscriptions/{id}"},
		{"PUT", "/api/admin/subscriptions/{id}/nodes"},
		{"POST", "/api/admin/subscriptions/{id}/rotate-link"},
		{"POST", "/api/admin/subscriptions/{id}/revoke"},
		{"DELETE", "/api/admin/subscriptions/{id}"},
		{"PUT", "/api/admin/subscriptions/{id}/traffic-limit"},
		{"POST", "/api/admin/subscriptions/{id}/reset-traffic"},
		{"PUT", "/api/admin/subscriptions/{id}/traffic-reset-schedule"},
		{"GET", "/api/admin/traffic"},
		{"GET", "/api/admin/settings/devices"},
		{"PUT", "/api/admin/settings/devices"},
	}
	allowed := map[string]bool{}
	request := func(method, path, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString("{}"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
		return w
	}
	for _, route := range routes {
		allowed[route.method+" "+route.path] = true
		path := route.path
		for _, param := range []string{"{id}", "{deviceID}", "{nodeID}"} {
			path = strings.ReplaceAll(path, param, "11111111-1111-4111-8111-111111111111")
		}
		t.Run(route.method+route.path, func(t *testing.T) {
			w := request(route.method, path, token)
			if w.Code == 401 || w.Code == 403 || w.Code >= 500 {
				t.Fatalf("service route returned %d: %s", w.Code, w.Body.String())
			}
		})
	}
	err = chi.Walk(handler.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/admin/") || allowed[method+" "+route] {
			return nil
		}
		route = strings.ReplaceAll(route, "{id}", "11111111-1111-4111-8111-111111111111")
		w := request(method, route, token)
		if w.Code != 401 {
			t.Errorf("session-only %s %s accepted API key: %d", method, route, w.Code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAPIToken(t.Context(), "test-request", issued.ID); err != nil {
		t.Fatal(err)
	}
	if got := request("GET", "/api/admin/users", token).Code; got != 401 {
		t.Fatalf("deleted key: %d", got)
	}
	if got := request("GET", "/api/admin/users", "dementia_api_"+store.NewToken()).Code; got != 401 {
		t.Fatalf("unknown key: %d", got)
	}
}
