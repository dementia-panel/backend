package users

import (
	"encoding/json"
	"net/http"
	"strings"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
)

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.Users(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, users)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) {
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	user, err := h.store.User(r.Context(), id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, user)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string  `json:"username"`
		TelegramID *int64  `json:"telegram_id"`
		PlanID     *string `json:"plan_id"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
		httpx.Failure(w, http.StatusBadRequest, "invalid username")
		return
	}
	if req.TelegramID != nil && *req.TelegramID <= 0 {
		httpx.Failure(w, http.StatusBadRequest, "telegram_id must be positive")
		return
	}
	if req.PlanID != nil {
		if !httpx.ValidID(*req.PlanID) {
			httpx.Failure(w, http.StatusBadRequest, "invalid plan ID")
			return
		}
		token := store.NewToken()
		user, accessID, err := h.store.CreateUserWithSubscription(r.Context(), req.Username, req.TelegramID, *req.PlanID, token)
		if err != nil {
			httpx.HandleError(w, err)
			return
		}
		httpx.Respond(w, http.StatusCreated, struct {
			store.User
			SubscriptionURL string `json:"subscription_url"`
			AccessID        string `json:"access_id"`
		}{user, h.publicURL + "/s/" + token, accessID})
		return
	}
	u, err := h.store.CreateUser(r.Context(), req.Username, req.TelegramID)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	var req struct {
		Username   *string         `json:"username"`
		TelegramID json.RawMessage `json:"telegram_id"`
		Enabled    *bool           `json:"enabled"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Username == nil && req.TelegramID == nil && req.Enabled == nil {
		httpx.Failure(w, http.StatusBadRequest, "provide username, telegram_id or enabled")
		return
	}
	if req.Username != nil {
		*req.Username = strings.TrimSpace(*req.Username)
		if !validUsername(*req.Username) {
			httpx.Failure(w, http.StatusBadRequest, "invalid username")
			return
		}
	}
	var telegramID *int64
	if req.TelegramID != nil {
		if err := json.Unmarshal(req.TelegramID, &telegramID); err != nil || (telegramID != nil && *telegramID <= 0) {
			httpx.Failure(w, http.StatusBadRequest, "telegram_id must be a positive integer or null")
			return
		}
	}
	if err := h.store.UpdateUser(r.Context(), id, req.Username, telegramID, req.TelegramID != nil, req.Enabled); err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}

func validUsername(username string) bool {
	return username != "" && len(username) <= 128 && !strings.ContainsRune(username, 0)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	if err := h.store.DeleteUser(r.Context(), id); err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}

func (h *Handler) bulkEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs     []string `json:"ids"`
		Enabled *bool    `json:"enabled"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Enabled == nil || len(req.IDs) == 0 || len(req.IDs) > 500 {
		httpx.Failure(w, http.StatusBadRequest, "provide 1–500 user IDs and enabled")
		return
	}
	seen := make(map[string]bool, len(req.IDs))
	for index, value := range req.IDs {
		id := httpx.UserReference(w, r, h.store, value)
		if id == "" {
			return
		}
		req.IDs[index] = id
		if seen[id] {
			httpx.Failure(w, http.StatusBadRequest, "provide unique user IDs")
			return
		}
		seen[id] = true
		req.IDs[index] = id
	}
	updated, err := h.store.BulkUserEnabled(r.Context(), req.IDs, *req.Enabled)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]int64{"updated": updated})
}
