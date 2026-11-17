package traffic

import (
	"dementia/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

type Handler struct{ store store.Store }

func Register(r chi.Router, repo store.Store) {
	h := &Handler{store: repo}
	r.Get("/traffic", h.history)
}
