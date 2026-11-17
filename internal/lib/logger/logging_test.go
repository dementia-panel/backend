package logger

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRequestsLogsRouteWithoutCredentials(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	router := chi.NewRouter()
	router.Use(Requests)
	router.Get("/s/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/s/test-subscription-secret?key=test-query-secret", nil)
	request.Header.Set("Authorization", "Bearer test-api-secret")
	router.ServeHTTP(httptest.NewRecorder(), request)
	line := output.String()
	if !strings.Contains(line, `"route":"/s/{token}"`) || !strings.Contains(line, `"status":204`) || strings.Contains(line, "-secret") {
		t.Fatalf("unexpected access log: %s", line)
	}
}
func TestRecoverRequestsDoesNotLogPanicValue(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	router := chi.NewRouter()
	router.Use(Requests, RecoverRequests)
	router.Get("/s/{token}", func(w http.ResponseWriter, r *http.Request) {
		panic([]string{"test-private-value"})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/s/test-private-path", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(output.String(), "test-private-") {
		t.Fatalf("unexpected status %d or leaked panic context", response.Code)
	}
}
