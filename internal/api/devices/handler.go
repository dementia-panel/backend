package devices

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
	r.Get("/users/{id}/devices", h.list)
	r.Post("/users/{id}/devices", h.create)
	r.Get("/users/{id}/devices/{deviceID}", h.detail)
	r.Get("/users/{id}/devices/{deviceID}/link", h.link)
	r.Patch("/users/{id}/devices/{deviceID}", h.rename)
	r.Delete("/users/{id}/devices/{deviceID}", h.remove)
	r.Put("/users/{id}/device-limit", h.limit)
	r.Get("/users/{id}/devices/{deviceID}/nodes/{nodeID}/profile", h.profile)
}
