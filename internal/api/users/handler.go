package users

import (
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store store.Store
}

func Register(r chi.Router, repo store.Store) {
	h := &Handler{store: repo}
	r.Get("/users", h.users)
	r.Post("/users", h.createUser)
	r.Get("/users/{id}", h.user)
	r.Patch("/users/{id}", h.updateUser)
	r.Delete("/users/{id}", h.deleteUser)
	r.Post("/users/bulk/enabled", h.bulkEnabled)
}
