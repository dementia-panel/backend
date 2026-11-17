package telegram

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const WebhookPath = "/api/telegram/webhook"

type Bot struct {
	status *integration.Tracker
	config Config
	repo   store.Store
	client client
}

func New(config Config, repo store.Store) *Bot {
	return &Bot{status: integration.NewTracker(config.Token != ""), config: config, repo: repo, client: client{baseURL: "https://api.telegram.org/bot" + config.Token, http: &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}}
}
func (b *Bot) Status() integration.Status {
	if b.status == nil {
		return integration.NewTracker(b.config.Token != "").Snapshot()
	}
	return b.status.Snapshot()
}
func (b *Bot) recordStatus(success bool) {
	if b.status != nil {
		b.status.Record(success)
	}
}
func (b *Bot) WebhookEnabled() bool {
	return b.config.Mode == "webhook"
}
func (b *Bot) Run(ctx context.Context, publicURL string) {
	defer b.client.http.CloseIdleConnections()
	for ctx.Err() == nil {
		setupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := b.configure(setupCtx, publicURL)
		cancel()
		if ctx.Err() == nil {
			b.recordStatus(err == nil)
		}
		if err == nil {
			if !b.WebhookEnabled() {
				b.poll(ctx)
			}
			return
		}
		if ctx.Err() == nil {
			slog.Warn("Telegram bot configuration failed", "mode", b.config.Mode, sl.Err(err))
		}
		pause(ctx, retryDelay(err, 5*time.Second))
	}
}
func (b *Bot) configure(ctx context.Context, publicURL string) error {
	if b.WebhookEnabled() {
		return b.client.call(ctx, "setWebhook", map[string]any{"url": publicURL + WebhookPath, "secret_token": b.config.WebhookSecret, "allowed_updates": []string{"message"}, "max_connections": 1}, nil)
	}
	return b.client.call(ctx, "deleteWebhook", struct{}{}, nil)
}

type update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Text     string `json:"text"`
		ThreadID int64  `json:"message_thread_id"`
		Chat     struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func (b *Bot) poll(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		var updates []update
		err := b.client.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "limit": 20, "allowed_updates": []string{"message"}}, &updates)
		if err != nil {
			if ctx.Err() == nil {
				b.recordStatus(false)
				slog.Warn("Telegram polling failed", sl.Err(err))
			}
			pause(ctx, retryDelay(err, 5*time.Second))
			continue
		}
		b.recordStatus(true)
		for _, item := range updates {
			if err := b.handleUpdate(ctx, item); err != nil {
				if ctx.Err() == nil {
					slog.Warn("Telegram command failed", sl.Err(err))
				}
				pause(ctx, retryDelay(err, 5*time.Second))
				break
			}
			offset = item.ID + 1
		}
	}
}
func (b *Bot) Webhook(w http.ResponseWriter, r *http.Request) {
	const op = "internal/telegram/Webhook"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling Telegram webhook")
	expected := sha256.Sum256([]byte(b.config.WebhookSecret))
	provided := sha256.Sum256([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")))
	if !b.WebhookEnabled() || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
		httpx.Failure(w, r, http.StatusForbidden, "invalid Telegram webhook credentials")
		return
	}
	var item update
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		httpx.Failure(w, r, http.StatusBadRequest, "invalid Telegram update")
		return
	}
	if err := b.handleUpdate(r.Context(), item); err != nil {
		log.WarnContext(r.Context(), "Telegram command failed", sl.Err(err))
		httpx.Failure(w, r, http.StatusServiceUnavailable, "Telegram command temporarily unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (b *Bot) handleUpdate(ctx context.Context, item update) error {
	message := item.Message
	if message == nil || message.Chat.ID != b.config.ChatID || (b.config.ThreadID != 0 && b.config.ThreadID != message.ThreadID) {
		return nil
	}
	fields := strings.Fields(message.Text)
	if len(fields) == 0 {
		return nil
	}
	command, _, _ := strings.Cut(fields[0], "@")
	switch command {
	case "/start", "/help":
		return b.send(ctx, "/nodes — состояние нод и трафик за сегодня (UTC).")
	case "/nodes", "/status":
		rows, err := b.repo.TelegramNodeMetrics(ctx, middleware.GetReqID(ctx))
		if err != nil {
			return errors.New("could not read node metrics")
		}
		return b.send(ctx, formatMetrics(rows))
	default:
		return nil
	}
}
func (b *Bot) send(ctx context.Context, text string) error {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	payload := map[string]any{"chat_id": b.config.ChatID, "text": text}
	if b.config.ThreadID != 0 {
		payload["message_thread_id"] = b.config.ThreadID
	}
	err := b.client.call(ctx, "sendMessage", payload, nil)
	if parent.Err() == nil {
		b.recordStatus(err == nil)
	}
	return err
}
func formatMetrics(rows []store.TelegramNodeMetric) string {
	if len(rows) == 0 {
		return "Ноды пока не зарегистрированы."
	}
	var text strings.Builder
	text.WriteString("Ноды · трафик за сегодня (UTC)\n")
	shown := 0
	for _, row := range rows[:min(len(rows), 50)] {
		status := row.Status
		if !row.Enabled {
			status = "пауза"
		} else if status == "ready" {
			status = "нет связи"
			if row.Online {
				status = "в сети"
			}
		}
		line := fmt.Sprintf("\n%s · %s · ↓ %.2f / ↑ %.2f МиБ", row.Name, status, float64(row.Received)/(1<<20), float64(row.Sent)/(1<<20))
		if text.Len()+len(line) > 3800 {
			break
		}
		text.WriteString(line)
		shown++
	}
	if shown < len(rows) {
		text.WriteString("\nПоказана часть списка.")
	}
	return text.String()
}
func retryDelay(err error, fallback time.Duration) time.Duration {
	var response *apiError
	if errors.As(err, &response) {
		return max(fallback, response.retryAfter)
	}
	return fallback
}
func pause(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
