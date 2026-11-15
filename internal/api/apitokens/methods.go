package apitokens

import (
	"net/http"
	"strings"
	"unicode"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.store.APITokens(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, tokens)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
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
	token, err := h.store.CreateAPIToken(r.Context(), name, raw)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, struct {
		store.APIToken
		Token string `json:"token"`
	}{token, raw})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	if err := h.store.DeleteAPIToken(r.Context(), id); err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
