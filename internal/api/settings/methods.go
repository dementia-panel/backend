package settings

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/store"
)

func (h *Handler) integrations(w http.ResponseWriter, r *http.Request) {
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
	branding, err := h.store.SubscriptionBranding(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, branding)
}

func (h *Handler) updateBranding(w http.ResponseWriter, r *http.Request) {
	var req store.SubscriptionBranding
	r.Body = http.MaxBytesReader(w, r.Body, int64(base64.StdEncoding.EncodedLen(maxLogoBytes)+4096))
	if !httpx.DecodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 64 || strings.ContainsFunc(req.Name, unicode.IsControl) {
		httpx.Failure(w, http.StatusBadRequest, "invalid page name")
		return
	}
	if req.Logo != "" {
		contentType, data, ok := strings.Cut(req.Logo, ";base64,")
		if !ok || (contentType != "data:image/png" && contentType != "data:image/jpeg") {
			httpx.Failure(w, http.StatusBadRequest, "invalid page logo")
			return
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(data)
		if err != nil || len(raw) > maxLogoBytes {
			httpx.Failure(w, http.StatusBadRequest, "invalid page logo")
			return
		}
		size, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || contentType != "data:image/"+format || size.Width < 1 || size.Height < 1 || size.Width > 2048 || size.Height > 2048 {
			httpx.Failure(w, http.StatusBadRequest, "invalid page logo")
			return
		}
		if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
			httpx.Failure(w, http.StatusBadRequest, "invalid page logo")
			return
		}
		req.Logo = contentType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	}
	if err := h.store.SaveSubscriptionBranding(r.Context(), req.Name, req.Logo); err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, req)
}
