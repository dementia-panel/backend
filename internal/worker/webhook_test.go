package worker

import (
	"crypto/hmac"
	"crypto/sha256"
	"dementia/backend/internal/store"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
func TestSendWebhook(t *testing.T) {
	const secret = "test-secret"
	event := store.Event{ID: 42, Scope: "node", Name: "node.connection_lost", Timestamp: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), Data: json.RawMessage(`{"online":false}`)}
	for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable} {
		client := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			if req.Method != http.MethodPost || req.Header.Get("X-Remnawave-Signature") != hex.EncodeToString(mac.Sum(nil)) || req.Header.Get("X-Remnawave-Timestamp") != event.Timestamp.Format(time.RFC3339Nano) {
				t.Error("unexpected webhook method or signature headers")
			}
			var received store.Event
			if err := json.Unmarshal(body, &received); err != nil || received.ID != event.ID || received.Name != event.Name {
				t.Errorf("unexpected webhook payload: %s", body)
			}
			return &http.Response{StatusCode: status, Body: http.NoBody}, nil
		})}
		err := sendWebhook(t.Context(), client, "https://webhook.example.test", secret, event)
		if (err != nil) != (status != http.StatusNoContent) {
			t.Errorf("HTTP %d: got %v", status, err)
		}
	}
}
