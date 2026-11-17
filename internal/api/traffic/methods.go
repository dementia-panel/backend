package traffic

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5/middleware"
)

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/traffic/history"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	query := r.URL.Query()
	nodeID, userID := query.Get("node_id"), query.Get("user_id")
	if telegram := query.Get("telegram_id"); telegram != "" {
		if userID != "" {
			httpx.Failure(w, r, 400, "provide user_id or telegram_id, not both")
			return
		}
		userID = telegram
	}
	if userID != "" {
		userID = httpx.UserReference(w, r, h.store, userID)
		if userID == "" {
			return
		}
	}
	if nodeID != "" && !httpx.ValidID(nodeID) {
		httpx.Failure(w, r, http.StatusBadRequest, "invalid node_id or user_id")
		return
	}
	days := 30
	if value := query.Get("days"); value != "" {
		var err error
		days, err = strconv.Atoi(value)
		if err != nil || days < 1 || days > 366 {
			httpx.Failure(w, r, http.StatusBadRequest, "days must be between 1 and 366")
			return
		}
	}
	history, err := h.store.TrafficHistory(r.Context(), requestID, nodeID, userID, days)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, http.StatusOK, history)
}
