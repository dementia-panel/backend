package devices

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/list"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	result, err := h.store.Devices(r.Context(), requestID, id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, result)
}
func deviceName(w http.ResponseWriter, r *http.Request) (string, bool) {
	const op = "internal/api/devices/deviceName"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &req) {
		return "", false
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || !utf8.ValidString(name) || len(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		httpx.Failure(w, r, 400, "device name must contain 1–128 bytes without control characters")
		return "", false
	}
	return name, true
}
func deviceID(w http.ResponseWriter, r *http.Request) string {
	const op = "internal/api/devices/deviceID"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := chi.URLParam(r, "deviceID")
	if !httpx.ValidID(id) {
		httpx.Failure(w, r, 400, "invalid device ID")
		return ""
	}
	return id
}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/create"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.UserIDParam(w, r, h.store)
	if id == "" {
		return
	}
	name, ok := deviceName(w, r)
	if !ok {
		return
	}
	result, err := h.store.CreateDevice(r.Context(), requestID, id, name)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 201, result)
}
func (h *Handler) rename(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/rename"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	id := deviceID(w, r)
	if id == "" {
		return
	}
	name, ok := deviceName(w, r)
	if !ok {
		return
	}
	if err := h.store.RenameDevice(r.Context(), requestID, user, id, name); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 204, nil)
}
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/remove"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	id := deviceID(w, r)
	if id == "" {
		return
	}
	if err := h.store.DeleteDevice(r.Context(), requestID, user, id); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 204, nil)
}
func (h *Handler) limit(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/limit"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	var req struct {
		Limit json.RawMessage `json:"device_limit"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	var limit *int
	if len(req.Limit) == 0 || json.Unmarshal(req.Limit, &limit) != nil || (limit != nil && (*limit < 1 || *limit > 10000)) {
		httpx.Failure(w, r, 400, "device_limit must be 1–10000 or null")
		return
	}
	if err := h.store.SetUserDeviceLimit(r.Context(), requestID, user, limit); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 204, nil)
}
func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/profile"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	device := deviceID(w, r)
	if device == "" {
		return
	}
	node := chi.URLParam(r, "nodeID")
	if !httpx.ValidID(node) {
		httpx.Failure(w, r, 400, "invalid node ID")
		return
	}
	profile, err := h.store.UserDeviceProfile(r.Context(), requestID, user, device, node)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	key, err := profile.AmneziaLink()
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]string{"conf": profile.Conf(), "key": key})
}
func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/detail"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	id := deviceID(w, r)
	if id == "" {
		return
	}
	device, err := h.store.Device(r.Context(), requestID, user, id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, device)
}
func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/devices/link"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	user := httpx.UserIDParam(w, r, h.store)
	if user == "" {
		return
	}
	device := deviceID(w, r)
	if device == "" {
		return
	}
	subscription := r.URL.Query().Get("subscription_id")
	if !httpx.ValidID(subscription) {
		httpx.Failure(w, r, 400, "invalid subscription_id")
		return
	}
	token, err := h.store.DeviceSubscriptionToken(r.Context(), requestID, user, device, subscription)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.Respond(w, 200, map[string]string{"url": h.publicURL + "/s/" + token})
}
