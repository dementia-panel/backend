package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func TestFailureLogsStatusReasonAndRequestWithoutCredentials(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			router := chi.NewRouter()
			router.Use(middleware.RequestID)
			router.Post("/resources/{id}", func(w http.ResponseWriter, r *http.Request) { Failure(w, r, status, "invalid resource settings") })
			request := httptest.NewRequest(http.MethodPost, "/resources/private-path-secret?token=private-query-secret", strings.NewReader(`{"password":"private-password-secret"}`))
			request.Header.Set("Authorization", "Bearer private-api-secret")
			request.Header.Set("Cookie", "session=private-cookie-secret")
			request.Header.Set("X-Request-ID", "test-request")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != status {
				t.Fatalf("response status: %d", response.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] != "invalid resource settings" {
				t.Fatalf("error response changed: %s", response.Body.String())
			}
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			expectedLevel := "WARN"
			if status >= 500 {
				expectedLevel = "ERROR"
			}
			if entry["level"] != expectedLevel || entry["request_id"] != "test-request" || entry["route"] != "/resources/{id}" || entry["status"] != float64(status) || entry["error"] != "invalid resource settings" {
				t.Fatalf("incomplete error log: %s", output.String())
			}
			if strings.Contains(output.String(), "-secret") {
				t.Fatal("request credentials leaked to logs")
			}
		})
	}
}

func TestMalformedJSONLogsOnlySafeReason(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	request := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{"password":"private-password-secret",`))
	response := httptest.NewRecorder()
	var body struct {
		Password string `json:"password"`
	}
	if Decode(response, request, &body) {
		t.Fatal("malformed JSON accepted")
	}
	if response.Code != 400 || !strings.Contains(output.String(), `"error":"invalid JSON"`) || strings.Contains(output.String(), "private-password-secret") {
		t.Fatalf("unexpected validation log: %s", output.String())
	}
}
