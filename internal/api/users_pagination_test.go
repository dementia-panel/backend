package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"

	"dementia/backend/internal/api"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
)

func TestUserPaginationAndSingleDeviceCreation(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, _ := vpn.NewVault(make([]byte, 32))
	repo := store.New(pool, vault)
	key := "dementia_api_" + store.NewToken()
	if _, err := repo.CreateAPIToken(t.Context(), "pagination-test", "External test", key); err != nil {
		t.Fatal(err)
	}
	handler := api.NewAPI(repo, make([]byte, 32), "https://panel.example.test", t.Context(), nil).Handler()
	call := func(method, path string, body any, status int, target any) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s: %d, expected %d; %s", method, path, response.Code, status, response.Body.String())
		}
		if target != nil {
			if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
				t.Fatal(err)
			}
		}
	}
	var first store.User
	for i := 0; i < 30; i++ {
		payload := map[string]any{"username": fmt.Sprintf("user-%02d", i)}
		if i == 0 {
			payload["telegram_id"] = 123456789
		}
		var user store.User
		call("POST", "/api/admin/users", payload, 201, &user)
		if i == 0 {
			first = user
		}
	}
	for i := 0; i < 2; i++ {
		call("POST", "/api/admin/users", map[string]any{"username": fmt.Sprintf("extra-user-%d", i)}, 201, nil)
	}
	var page store.UserPage
	call("GET", "/api/admin/users", nil, 200, &page)
	if len(page.Items) != 25 || page.Total != 32 || page.TotalPages != 2 || page.Page != 1 || page.PerPage != 25 || page.OnlineTotal != 0 {
		t.Fatalf("default page: %+v", page)
	}
	if page.Items[0].Username != "extra-user-1" {
		t.Fatal("default sort must show newest users first")
	}
	var ordered store.UserPage
	call("GET", "/api/admin/users?sort=created_at_asc&per_page=1", nil, 200, &ordered)
	if ordered.Items[0].ID != first.ID {
		t.Fatal("oldest-first sort failed")
	}
	call("GET", "/api/admin/users?sort=username&per_page=1", nil, 200, &ordered)
	if ordered.Items[0].Username != "extra-user-0" {
		t.Fatal("username sort failed")
	}
	call("GET", "/api/admin/users?sort=created_at&search=USER-2&page=2&per_page=3", nil, 200, &ordered)
	if ordered.Total != 10 || len(ordered.Items) != 3 || ordered.Items[0].Username != "user-26" {
		t.Fatal("created sort with filtering and pagination failed")
	}
	ids := map[string]bool{}
	for _, user := range page.Items {
		ids[user.ID] = true
	}
	call("GET", "/api/admin/users?page=2", nil, 200, &page)
	if len(page.Items) != 7 || page.Total != 32 {
		t.Fatalf("second page: %+v", page)
	}
	for _, user := range page.Items {
		if ids[user.ID] {
			t.Fatal("user duplicated across pages")
		}
		ids[user.ID] = true
	}
	if _, err := pool.Exec(t.Context(), "UPDATE users SET created_at='2030-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	expected := make([]string, 0, len(ids))
	for id := range ids {
		expected = append(expected, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(expected)))
	var actual []string
	for index := 1; index <= 4; index++ {
		call("GET", fmt.Sprintf("/api/admin/users?sort=created_at&page=%d&per_page=10", index), nil, 200, &ordered)
		for _, user := range ordered.Items {
			actual = append(actual, user.ID)
		}
	}
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		t.Fatal("created_at ties produced unstable pagination")
	}
	call("GET", "/api/admin/users?page=999&per_page=100", nil, 200, &page)
	if len(page.Items) != 0 || page.Items == nil || page.Total != 32 || page.TotalPages != 1 {
		t.Fatalf("out-of-range page: %+v", page)
	}
	call("GET", "/api/admin/users?search=USER-2&per_page=3&page=2&sort=username", nil, 200, &page)
	if len(page.Items) != 3 || page.Total != 10 || page.Items[0].Username != "user-23" {
		t.Fatalf("search before pagination: %+v", page)
	}
	call("GET", "/api/admin/users?search=123456789", nil, 200, &page)
	if page.Total != 1 || page.Items[0].ID != first.ID {
		t.Fatal("Telegram search failed")
	}
	call("GET", "/api/admin/users?search=%25", nil, 200, &page)
	if page.Total != 0 {
		t.Fatal("wildcard interpreted instead of literal substring")
	}
	call("GET", "/api/admin/users?search=not-found", nil, 200, &page)
	if page.Total != 0 || page.TotalPages != 0 || page.Items == nil {
		t.Fatal("empty result shape incorrect")
	}
	for _, query := range []string{"page=0", "page=-1", "page=abc", "page=1000000001", "page=1&page=2", "per_page=0", "per_page=101", "per_page=2&per_page=3", "per_page=", "status=invalid", "sort=invalid", "sort=created_at&sort=username", "search=" + url.QueryEscape(string(make([]byte, 129))), "search=%00"} {
		call("GET", "/api/admin/users?"+query, nil, 400, nil)
	}
	call("PATCH", "/api/admin/users/123456789", map[string]any{"enabled": false}, 204, nil)
	call("GET", "/api/admin/users?status=disabled", nil, 200, &page)
	if page.Total != 1 || page.Items[0].ID != first.ID || page.Items[0].Enabled {
		t.Fatal("disabled filter incorrect")
	}
	call("GET", "/api/admin/users?status=online", nil, 200, &page)
	if page.Total != 0 || page.OnlineTotal != 0 {
		t.Fatal("online filter incorrect")
	}
	call("GET", "/api/admin/users?sort=last_connected_at&per_page=1&page=1", nil, 200, &page)
	var next store.UserPage
	call("GET", "/api/admin/users?sort=last_connected_at&per_page=1&page=2", nil, 200, &next)
	if page.Items[0].ID == next.Items[0].ID {
		t.Fatal("unstable sort ties")
	}
	var device1, device2 store.Device
	path := "/api/admin/users/123456789/devices"
	call("POST", path, map[string]any{"name": "iPhone"}, 201, &device1)
	call("POST", path, map[string]any{"name": "Laptop"}, 201, &device2)
	if device1.ID == device2.ID {
		t.Fatal("new device replaced existing device")
	}
	var devices []store.Device
	call("GET", path, nil, 200, &devices)
	if len(devices) != 2 {
		t.Fatal("device creation lost earlier records")
	}
	names := map[string]string{}
	for _, device := range devices {
		names[device.ID] = device.Name
	}
	if names[device1.ID] != "iPhone" || names[device2.ID] != "Laptop" {
		t.Fatal("device details overwritten")
	}
	call("POST", path, []map[string]any{{"name": "Not a single object"}}, 400, nil)
	call("POST", "/api/admin/users", []map[string]any{{"username": "Not a single object"}}, 400, nil)
	call("POST", "/api/admin/users/bulk/enabled", map[string]any{"ids": []string{first.ID, "123456789"}, "enabled": true}, 400, nil)
	var user store.User
	call("GET", "/api/admin/users/"+first.ID, nil, 200, &user)
	if user.Enabled {
		t.Fatal("invalid duplicate-user bulk request modified user")
	}
	call("PATCH", "/api/admin/users/"+first.ID, map[string]any{"enabled": true}, 204, nil)
	call("GET", path, nil, 200, &devices)
	if len(devices) != 2 {
		t.Fatal("single-user update changed device records")
	}
}
