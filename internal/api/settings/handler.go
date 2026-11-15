package settings

import (
	"dementia/backend/internal/integration"
	"github.com/go-chi/chi/v5"
)

type Handler struct{ status func() integration.Snapshot }

func Register(r chi.Router, status func() integration.Snapshot) {
	h := &Handler{status: status}
	r.Get("/settings/integrations", h.integrations)
}
