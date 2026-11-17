package apitokens

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/apitokens/list"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	tokens, err := h.store.APITokens(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, tokens)
}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/apitokens/create"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		httpx.Failure(w, http.StatusBadRequest, "invalid API token name")
		return
	}
	raw := tokenPrefix + store.NewToken()
	token, err := h.store.CreateAPIToken(r.Context(), requestID, name, raw)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, struct {
		store.APIToken
		Token string `json:"token"`
	}{token, raw})
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/apitokens/delete"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	if err := h.store.DeleteAPIToken(r.Context(), requestID, id); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/apitokens/rename"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		httpx.Failure(w, 400, "invalid API token name")
		return
	}
	token, err := h.store.RenameAPIToken(r.Context(), requestID, id, name)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, token)
}
