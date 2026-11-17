package api_test

import (
	"bytes"
	"dementia/backend/internal/api"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTelegramSettingsRequireSessionAndPersist(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, _ := vpn.NewVault(make([]byte, 32))
	repo := store.New(pool, vault)
	const origin = "https://panel.example.test"
	handler := api.NewAPI(repo, make([]byte, 32), origin, t.Context(), nil).Handler()
	call := func(method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, origin+path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	setup := call("POST", "/api/auth/setup", `{"username":"owner","password":"private-admin-password"}`, nil)
	if setup.Code != 201 {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	cookies := setup.Result().Cookies()
	if got := call("GET", "/api/admin/settings/telegram", "", nil).Code; got != 401 {
		t.Fatalf("unauthenticated settings: %d", got)
	}
	before, err := repo.PendingTelegram(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	login := false
	for _, event := range before {
		if event.Event == "admin.login" {
			login = true
		}
		if strings.Contains(string(event.Data), "private-admin-password") {
			t.Fatal("password leaked to notification")
		}
	}
	if !login {
		t.Fatal("administrator login notification missing")
	}
	saved := call("PUT", "/api/admin/settings/telegram", `{"node":false,"user":true,"admin":false,"api_token":true,"subscription":false}`, cookies)
	if saved.Code != 200 {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	read := call("GET", "/api/admin/settings/telegram", "", cookies)
	var value store.TelegramSettings
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &value) != nil || value.Node || !value.User || value.Admin || !value.APIToken {
		t.Fatalf("preferences: %d %s", read.Code, read.Body.String())
	}
	if got := call("PUT", "/api/admin/settings/telegram", `{"unknown":true}`, cookies).Code; got != 400 {
		t.Fatalf("unknown category accepted: %d", got)
	}
	pending, err := repo.PendingTelegram(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range pending {
		if event.Event == "admin.login" {
			t.Fatal("disabled login category remained queued")
		}
	}
	token, err := repo.CreateAPIToken(t.Context(), "test", "Before", "private-service-token")
	if err != nil {
		t.Fatal(err)
	}
	renamed := call("PATCH", "/api/admin/api-tokens/"+token.ID, `{"name":"After"}`, cookies)
	if renamed.Code != 200 {
		t.Fatalf("rename API token: %d %s", renamed.Code, renamed.Body.String())
	}
	if got := call("PATCH", "/api/admin/api-tokens/"+token.ID, `{"name":""}`, cookies).Code; got != 400 {
		t.Fatalf("empty token name accepted: %d", got)
	}
}
