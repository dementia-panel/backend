package api_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"dementia/backend/internal/api"
	"dementia/backend/internal/store"
)

func TestEndpointRejectionsAreLogged(t *testing.T) {
	for _, test := range []struct {
		method, path, authorization string
		status                      int
	}{
		{"POST", "/api/agent/nodes/11111111-1111-4111-8111-111111111111/ack", "Bearer private-agent-secret", 403},
		{"GET", "/api/admin/users", "Bearer private-api-secret", 401},
		{"GET", "/api/admin/users", "", 401},
	} {
		t.Run(test.path+test.authorization, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			handler := api.NewAPI(store.Store{}, make([]byte, 32), "https://panel.example.test", t.Context(), nil).Handler()
			request := httptest.NewRequest(test.method, test.path+"?password=private-query-secret", nil)
			request.Header.Set("X-Request-ID", "endpoint-test")
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status: %d %s", response.Code, response.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["msg"] == "HTTP request rejected" {
					found = true
					if entry["request_id"] != "endpoint-test" || entry["level"] != "WARN" || entry["error"] != body["error"] {
						t.Fatalf("unexpected rejection log: %s", line)
					}
				}
			}
			if !found || strings.Contains(output.String(), "-secret") {
				t.Fatalf("missing rejection log or credentials leaked: %s", output.String())
			}
		})
	}
}
