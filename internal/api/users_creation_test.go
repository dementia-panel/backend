package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"dementia/backend/internal/api"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
)

func TestUserCreationAcceptsOptionalUUID(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, _ := vpn.NewVault(make([]byte, 32))
	repo := store.New(pool, vault)
	key := "dementia_api_" + store.NewToken()
	if _, err := repo.CreateAPIToken(t.Context(), "uuid-test", "UUID test", key); err != nil {
		t.Fatal(err)
	}
	handler := api.NewAPI(repo, make([]byte, 32), "https://panel.example.test", t.Context(), nil).Handler()
	call := func(body any, status int) store.User {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/api/admin/users", bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("status %d, want %d: %s", response.Code, status, response.Body.String())
		}
		var user store.User
		if status == 201 {
			if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil {
				t.Fatal(err)
			}
		}
		return user
	}
	generated := call(map[string]any{"username": "generated"}, 201)
	generatedNull := call(map[string]any{"username": "generated-null", "id": nil}, 201)
	if !httpx.ValidID(generated.ID) || !httpx.ValidID(generatedNull.ID) || generated.ID == generatedNull.ID {
		t.Fatal("UUID generation failed")
	}
	const id = "aabbccdd-1234-4234-8234-123456789abc"
	user := call(map[string]any{"username": "custom", "id": strings.ToUpper(id), "telegram_id": 123456789}, 201)
	if user.ID != id {
		t.Fatalf("custom UUID not preserved: %s", user.ID)
	}
	call(map[string]any{"username": "duplicate", "id": id}, 409)
	persisted, err := repo.User(t.Context(), "uuid-test", id)
	if err != nil || persisted.Username != "custom" {
		t.Fatal("duplicate request modified existing user", err)
	}
	for index, bad := range []any{"", "not-a-uuid", "aabbccdd123442348234123456789abc", " aabbccdd-1234-4234-8234-123456789abc", 123, []string{id}} {
		call(map[string]any{"username": fmt.Sprintf("invalid-%d", index), "id": bad}, 400)
	}
	plan, err := repo.SaveSubscriptionPlan(t.Context(), "uuid-test", "", "Test plan", []string{})
	if err != nil {
		t.Fatal(err)
	}
	const plannedID = "bbbbbbbb-1234-4234-8234-123456789abc"
	for _, requested := range []any{plannedID, nil} {
		payload := map[string]any{"username": "planned-" + fmt.Sprint(requested), "plan_id": plan.ID}
		if requested != nil {
			payload["id"] = requested
		}
		user := call(payload, 201)
		if !httpx.ValidID(user.ID) || (requested != nil && user.ID != requested) || user.SubscriptionPlanID == nil || *user.SubscriptionPlanID != plan.ID || user.DeviceCount != 1 {
			t.Fatalf("planned user: %+v", user)
		}
	}
	call(map[string]any{"username": "plan-duplicate", "id": id, "plan_id": plan.ID}, 409)
	const rollbackID = "cccccccc-1234-4234-8234-123456789abc"
	call(map[string]any{"username": "rollback", "id": rollbackID, "plan_id": "dddddddd-1234-4234-8234-123456789abc"}, 404)
	if user := call(map[string]any{"username": "rollback", "id": rollbackID}, 201); user.ID != rollbackID {
		t.Fatal("failed plan assignment did not roll back user")
	}
}
