package settings

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/integration"
	"net/http"
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
