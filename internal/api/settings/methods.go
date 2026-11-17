package settings

import (
	"bytes"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"encoding/base64"
	"encoding/json"
	"image"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"

	_ "image/jpeg"

	_ "image/png"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (h *Handler) integrations(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/integrations"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	disabled := integration.NewTracker(false).Snapshot()
	result := integration.Snapshot{Telegram: disabled, Webhook: disabled}
	if h.status != nil {
		result = h.status()
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.Respond(w, http.StatusOK, result)
}

const maxLogoBytes = 256 << 10

func (h *Handler) branding(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/branding"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	branding, err := h.store.SubscriptionBranding(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, branding)
}
func (h *Handler) updateBranding(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/updateBranding"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req store.SubscriptionBranding
	r.Body = http.MaxBytesReader(w, r.Body, int64(base64.StdEncoding.EncodedLen(maxLogoBytes)+4096))
	if !httpx.DecodeBody(w, r, &req) {
		return
	}
	if req.DefaultLanguage == "" {
		current, err := h.store.SubscriptionBranding(r.Context(), requestID)
		if err != nil {
			httpx.HandleError(w, r, err)
			return
		}
		req.DefaultLanguage = current.DefaultLanguage
	}
	if req.DefaultLanguage != "en" && req.DefaultLanguage != "ru" {
		httpx.Failure(w, r, 400, "invalid default language")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 64 || strings.ContainsFunc(req.Name, unicode.IsControl) {
		httpx.Failure(w, r, http.StatusBadRequest, "invalid page name")
		return
	}
	if req.Logo != "" {
		contentType, data, ok := strings.Cut(req.Logo, ";base64,")
		if !ok || (contentType != "data:image/png" && contentType != "data:image/jpeg") {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid page logo")
			return
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(data)
		if err != nil || len(raw) > maxLogoBytes {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid page logo")
			return
		}
		size, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || contentType != "data:image/"+format || size.Width < 1 || size.Height < 1 || size.Width > 2048 || size.Height > 2048 {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid page logo")
			return
		}
		if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid page logo")
			return
		}
		req.Logo = contentType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	}
	if err := h.store.SaveSubscriptionBranding(r.Context(), requestID, req.Name, req.Logo, req.DefaultLanguage); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, req)
}
func (h *Handler) deviceSettings(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/deviceSettings"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	limit, err := h.store.DefaultDeviceLimit(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]*int{"default_device_limit": limit})
}
func (h *Handler) saveDeviceSettings(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/saveDeviceSettings"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		Limit json.RawMessage `json:"default_device_limit"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	var limit *int
	if len(req.Limit) == 0 || json.Unmarshal(req.Limit, &limit) != nil || (limit != nil && (*limit < 1 || *limit > 10000)) {
		httpx.Failure(w, r, 400, "default_device_limit must be 1–10000 or null")
		return
	}
	if err := h.store.SetDefaultDeviceLimit(r.Context(), requestID, limit); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]*int{"default_device_limit": limit})
}
func (h *Handler) telegramSettings(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/telegramSettings"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	value, err := h.store.TelegramSettings(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, value)
}
func (h *Handler) saveTelegramSettings(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/settings/saveTelegramSettings"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var value store.TelegramSettings
	if !httpx.Decode(w, r, &value) {
		return
	}
	if err := h.store.SaveTelegramSettings(r.Context(), requestID, value); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, value)
}
