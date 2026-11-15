package traffic

import (
	"net/http"
	"strconv"

	"dementia/backend/internal/api/httpx"
)

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nodeID, userID := query.Get("node_id"), query.Get("user_id")
	if (nodeID != "" && !httpx.ValidID(nodeID)) || (userID != "" && !httpx.ValidID(userID)) {
		httpx.Failure(w, http.StatusBadRequest, "invalid node_id or user_id")
		return
	}
	days := 30
	if value := query.Get("days"); value != "" {
		var err error
		days, err = strconv.Atoi(value)
		if err != nil || days < 1 || days > 366 {
			httpx.Failure(w, http.StatusBadRequest, "days must be between 1 and 366")
			return
		}
	}
	history, err := h.store.TrafficHistory(r.Context(), nodeID, userID, days)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, history)
}
