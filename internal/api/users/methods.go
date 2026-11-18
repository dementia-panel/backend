package users

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/users"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	query := store.UserQuery{Page: 1, PerPage: 25, Status: "all", Sort: "username"}
	params := r.URL.Query()
	for _, param := range []struct {
		name  string
		value *int
		max   int
	}{{"page", &query.Page, 1000000000}, {"per_page", &query.PerPage, 100}} {
		if values, ok := params[param.name]; ok {
			if len(values) != 1 {
				httpx.Failure(w, r, 400, "provide one value for "+param.name)
				return
			}
			value, err := strconv.ParseUint(values[0], 10, 32)
			if err != nil || value < 1 || value > uint64(param.max) {
				httpx.Failure(w, r, 400, "invalid "+param.name)
				return
			}
			*param.value = int(value)
		}
	}
	if values, ok := params["search"]; ok {
		if len(values) != 1 || len(values[0]) > 128 || strings.ContainsRune(values[0], 0) {
			httpx.Failure(w, r, 400, "invalid search")
			return
		}
		query.Search = strings.TrimSpace(values[0])
	}
	if values, ok := params["status"]; ok {
		if len(values) != 1 || (values[0] != "all" && values[0] != "online" && values[0] != "disabled") {
			httpx.Failure(w, r, 400, "invalid status")
			return
		}
		query.Status = values[0]
	}
	if values, ok := params["sort"]; ok {
		if len(values) != 1 || (values[0] != "username" && values[0] != "last_connected_at") {
			httpx.Failure(w, r, 400, "invalid sort")
			return
		}
		query.Sort = values[0]
	}
	users, err := h.store.UsersPage(r.Context(), requestID, query)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, users)
}
func (h *Handler) user(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/user"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	user, err := h.store.User(r.Context(), requestID, id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, user)
}
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/createUser"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		ID         *string `json:"id"`
		Username   string  `json:"username"`
		TelegramID *int64  `json:"telegram_id"`
		PlanID     *string `json:"plan_id"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.ID != nil {
		*req.ID = strings.ToLower(*req.ID)
		if !httpx.ValidID(*req.ID) {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid user ID")
			return
		}
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
		httpx.Failure(w, r, http.StatusBadRequest, "invalid username")
		return
	}
	if req.TelegramID != nil && *req.TelegramID <= 0 {
		httpx.Failure(w, r, http.StatusBadRequest, "telegram_id must be positive")
		return
	}
	if req.PlanID != nil {
		if !httpx.ValidID(*req.PlanID) {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid plan ID")
			return
		}
		token := store.NewToken()
		user, accessID, err := h.store.CreateUserWithSubscription(r.Context(), requestID, req.Username, req.TelegramID, *req.PlanID, token, req.ID)
		if err != nil {
			httpx.HandleError(w, r, err)
			return
		}
		httpx.Respond(w, http.StatusCreated, struct {
			store.User
			SubscriptionURL string `json:"subscription_url"`
			AccessID        string `json:"access_id"`
		}{user, h.publicURL + "/s/" + token, accessID})
		return
	}
	u, err := h.store.CreateUser(r.Context(), requestID, req.Username, req.TelegramID, req.ID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, u)
}
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/updateUser"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
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
		httpx.Failure(w, r, http.StatusBadRequest, "provide username, telegram_id or enabled")
		return
	}
	if req.Username != nil {
		*req.Username = strings.TrimSpace(*req.Username)
		if !validUsername(*req.Username) {
			httpx.Failure(w, r, http.StatusBadRequest, "invalid username")
			return
		}
	}
	var telegramID *int64
	if req.TelegramID != nil {
		if err := json.Unmarshal(req.TelegramID, &telegramID); err != nil || (telegramID != nil && *telegramID <= 0) {
			httpx.Failure(w, r, http.StatusBadRequest, "telegram_id must be a positive integer or null")
			return
		}
	}
	if err := h.store.UpdateUser(r.Context(), requestID, id, req.Username, telegramID, req.TelegramID != nil, req.Enabled); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func validUsername(username string) bool {
	return username != "" && len(username) <= 128 && !strings.ContainsRune(username, 0)
}
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/deleteUser"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	if err := h.store.DeleteUser(r.Context(), requestID, id); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) bulkEnabled(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/users/bulkEnabled"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		IDs     []string `json:"ids"`
		Enabled *bool    `json:"enabled"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Enabled == nil || len(req.IDs) == 0 || len(req.IDs) > 500 {
		httpx.Failure(w, r, http.StatusBadRequest, "provide 1–500 user IDs and enabled")
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
			httpx.Failure(w, r, http.StatusBadRequest, "provide unique user IDs")
			return
		}
		seen[id] = true
		req.IDs[index] = id
	}
	updated, err := h.store.BulkUserEnabled(r.Context(), requestID, req.IDs, *req.Enabled)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]int64{"updated": updated})
}
