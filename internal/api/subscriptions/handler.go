package subscriptions

import (
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store     store.Store
	publicURL string
}

func Register(r chi.Router, repo store.Store, publicURL string) {
	h := &Handler{store: repo, publicURL: publicURL}
	r.Get("/subscriptions", h.subscriptions)
	r.Post("/subscriptions", h.createSubscription)
	r.Patch("/subscriptions/{id}", h.updateSubscription)
	r.Put("/subscriptions/{id}/nodes", h.replaceSubscriptionNodes)
	r.Post("/subscriptions/{id}/rotate-link", h.rotateLink)
	r.Delete("/subscriptions/{id}", h.deleteSubscription)
	r.Post("/subscriptions/{id}/revoke", h.revokeSubscription)
	r.Put("/subscriptions/{id}/traffic-limit", h.setTrafficLimit)
	r.Post("/subscriptions/{id}/reset-traffic", h.resetTraffic)
	r.Put("/subscriptions/{id}/traffic-reset-schedule", h.setTrafficResetSchedule)
}
