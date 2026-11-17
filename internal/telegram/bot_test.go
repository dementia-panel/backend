package telegram

import (
	"context"
	"dementia/backend/internal/store"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
func TestWebhookAndPollingRegistration(t *testing.T) {
	for _, mode := range []string{"webhook", "polling"} {
		t.Run(mode, func(t *testing.T) {
			bot := New(Config{Mode: mode, ChatID: -100123, ThreadID: 7, WebhookSecret: "test-secret"}, store.Store{})
			var calls []string
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			bot.client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				calls = append(calls, method)
				body := `{"ok":true,"result":true}`
				switch method {
				case "setWebhook":
					if payload["url"] != "https://panel.example.test"+WebhookPath || payload["secret_token"] != "test-secret" {
						t.Fatal("wrong registration payload")
					}
				case "getUpdates":
					cancel()
					body = `{"ok":true,"result":[]}`
				case "sendMessage":
					if payload["chat_id"] != float64(-100123) || payload["message_thread_id"] != float64(7) {
						t.Fatal("wrong recipient")
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			bot.Run(ctx, "https://panel.example.test")
			if mode == "polling" {
				if strings.Join(calls, ",") != "deleteWebhook,getUpdates" {
					t.Fatalf("calls: %v", calls)
				}
				return
			}
			for _, test := range []struct {
				secret   string
				chat     string
				expected int
			}{{"wrong", "-100123", 403}, {"test-secret", "-100999", 204}, {"test-secret", "-100123", 204}} {
				r := httptest.NewRequest(http.MethodPost, WebhookPath, strings.NewReader(`{"message":{"text":"/help","message_thread_id":7,"chat":{"id":`+test.chat+`}}}`))
				r.Header.Set("X-Telegram-Bot-Api-Secret-Token", test.secret)
				w := httptest.NewRecorder()
				bot.Webhook(w, r)
				if w.Code != test.expected {
					t.Fatalf("status %d, want %d", w.Code, test.expected)
				}
			}
			if strings.Join(calls, ",") != "setWebhook,sendMessage" {
				t.Fatalf("calls: %v", calls)
			}
		})
	}
}
