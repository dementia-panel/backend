package settings

import (
	"dementia/backend/internal/integration"
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store  store.Store
	status func() integration.Snapshot
}

func Register(r chi.Router, repo store.Store, status func() integration.Snapshot) {
	h := &Handler{store: repo, status: status}
	r.Get("/settings/integrations", h.integrations)
	r.Get("/settings/subscription-branding", h.branding)
	r.Put("/settings/subscription-branding", h.updateBranding)
}
