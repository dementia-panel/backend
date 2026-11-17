package subscriptions

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) exportProfile(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/exportProfile"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	w.Header().Set("Cache-Control", "no-store")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	nodeID := chi.URLParam(r, "nodeID")
	if !httpx.ValidID(nodeID) {
		httpx.Failure(w, http.StatusBadRequest, "invalid node ID")
		return
	}
	deviceID := r.URL.Query().Get("device_id")
	if deviceID != "" && !httpx.ValidID(deviceID) {
		httpx.Failure(w, 400, "invalid device ID")
		return
	}
	profile, err := h.store.ProfileForSubscription(r.Context(), requestID, id, nodeID, deviceID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	key, err := profile.AmneziaLink()
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, struct {
		Conf string `json:"conf"`
		Key  string `json:"key"`
	}{profile.Conf(), key})
}
func (h *Handler) subscriptions(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/subscriptions"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	items, err := h.store.Subscriptions(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, items)
}
func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/createSubscription"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		UserID    string    `json:"user_id"`
		Name      string    `json:"name"`
		ExpiresAt time.Time `json:"expires_at"`
		NodeIDs   []string  `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !httpx.ValidID(req.UserID) || req.Name == "" || len(req.Name) > 128 || !req.ExpiresAt.After(time.Now()) || len(req.NodeIDs) == 0 || len(req.NodeIDs) > 128 {
		httpx.Failure(w, 400, "user, name, future expiration and 1–128 nodes are required")
		return
	}
	if !validateNodes(w, req.NodeIDs) {
		return
	}
	token := store.NewToken()
	sub, err := h.store.CreateSubscription(r.Context(), requestID, req.UserID, req.Name, token, req.ExpiresAt, req.NodeIDs)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 201, struct {
		store.Subscription
		URL string `json:"url"`
	}{sub, h.publicURL + "/s/" + token})
}
func validateNodes(w http.ResponseWriter, nodes []string) bool {
	if len(nodes) == 0 || len(nodes) > 128 {
		httpx.Failure(w, 400, "provide 1–128 nodes")
		return false
	}
	for _, id := range nodes {
		if !httpx.ValidID(id) {
			httpx.Failure(w, 400, "invalid node ID")
			return false
		}
	}
	slices.Sort(nodes)
	for i := 1; i < len(nodes); i++ {
		if nodes[i-1] == nodes[i] {
			httpx.Failure(w, 400, "duplicate node ID")
			return false
		}
	}
	return true
}
func (h *Handler) replaceSubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/replaceSubscriptionNodes"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		NodeIDs []string `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &req) || !validateNodes(w, req.NodeIDs) {
		return
	}
	if err := h.store.ReplaceSubscriptionNodes(r.Context(), requestID, id, req.NodeIDs); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) updateSubscription(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/updateSubscription"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Enabled   *bool      `json:"enabled"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Enabled == nil && req.ExpiresAt == nil {
		httpx.Failure(w, 400, "provide enabled or expires_at")
		return
	}
	if err := h.store.UpdateSubscription(r.Context(), requestID, id, req.Enabled, req.ExpiresAt); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 204, nil)
}
func (h *Handler) rotateLink(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/rotateLink"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	token := store.NewToken()
	if err := h.store.RotateToken(r.Context(), requestID, id, token); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]string{"url": h.publicURL + "/s/" + token})
}
func (h *Handler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/deleteSubscription"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	if err := h.store.DeleteSubscription(r.Context(), requestID, id); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) revokeSubscription(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/revokeSubscription"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	token := store.NewToken()
	if err := h.store.RevokeSubscription(r.Context(), requestID, id, token); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]string{"url": h.publicURL + "/s/" + token})
}
func (h *Handler) setTrafficLimit(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/setTrafficLimit"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Limit json.RawMessage `json:"traffic_limit_bytes"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	var limit *int64
	if req.Limit == nil || json.Unmarshal(req.Limit, &limit) != nil || (limit != nil && *limit <= 0) {
		httpx.Failure(w, http.StatusBadRequest, "provide a positive traffic_limit_bytes or null")
		return
	}
	if err := h.store.SetTrafficLimit(r.Context(), requestID, id, limit, false); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) resetTraffic(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/resetTraffic"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	if err := h.store.SetTrafficLimit(r.Context(), requestID, id, nil, true); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) setTrafficResetSchedule(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/setTrafficResetSchedule"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Days json.RawMessage `json:"traffic_reset_days"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	var days *int
	if req.Days == nil || json.Unmarshal(req.Days, &days) != nil || (days != nil && (*days < 1 || *days > 366)) {
		httpx.Failure(w, http.StatusBadRequest, "provide traffic_reset_days between 1 and 366, or null")
		return
	}
	if err := h.store.SetTrafficResetSchedule(r.Context(), requestID, id, days); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) plans(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/plans"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	result, err := h.store.SubscriptionPlans(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, result)
}
func (h *Handler) savePlan(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/savePlan"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var id string
	if r.Method == http.MethodPut {
		id = httpx.IDParam(w, r)
		if id == "" {
			return
		}
	}
	var req struct {
		Name    string   `json:"name"`
		NodeIDs []string `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		httpx.Failure(w, http.StatusBadRequest, "invalid subscription name")
		return
	}
	if !validateNodes(w, req.NodeIDs) {
		return
	}
	plan, err := h.store.SaveSubscriptionPlan(r.Context(), requestID, id, req.Name, req.NodeIDs)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, plan)
}
func (h *Handler) deletePlan(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/deletePlan"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	err := h.store.DeleteSubscriptionPlan(r.Context(), requestID, id)
	if errors.Is(err, store.ErrPlanInUse) {
		httpx.Failure(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusNoContent, nil)
}
func (h *Handler) assignPlan(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/assignPlan"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	var req struct {
		PlanID    json.RawMessage `json:"plan_id"`
		Enabled   *bool           `json:"enabled"`
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	var planID *string
	if len(req.PlanID) == 0 || json.Unmarshal(req.PlanID, &planID) != nil || (planID != nil && !httpx.ValidID(*planID)) {
		httpx.Failure(w, http.StatusBadRequest, "plan_id must be a UUID or null")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && json.Unmarshal(req.ExpiresAt, &expiresAt) != nil {
		httpx.Failure(w, http.StatusBadRequest, "expires_at must be an RFC3339 timestamp or null")
		return
	}
	token := store.NewToken()
	subID, issued, err := h.store.AssignSubscriptionPlanAccess(r.Context(), requestID, id, planID, token, req.Enabled, expiresAt, req.ExpiresAt != nil)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	result := struct {
		ID  string `json:"id,omitempty"`
		URL string `json:"url,omitempty"`
	}{ID: subID}
	if issued {
		result.URL = h.publicURL + "/s/" + token
	}
	httpx.Respond(w, http.StatusOK, result)
}
func (h *Handler) assignedPlan(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/assignedPlan"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	sub, err := h.store.AssignedSubscription(r.Context(), requestID, id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, sub)
}
func (h *Handler) subscriptionLink(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/subscriptions/subscriptionLink"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	token, err := h.store.SubscriptionToken(r.Context(), requestID, id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	var link *string
	if token != "" {
		value := h.publicURL + "/s/" + token
		link = &value
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.Respond(w, http.StatusOK, map[string]*string{"url": link})
}
