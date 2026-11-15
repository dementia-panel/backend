package agent

import (
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store   store.Store
	reports chan struct{}
}

func Register(r chi.Router, repo store.Store) {
	h := &Handler{store: repo, reports: make(chan struct{}, 4)}
	r.Get("/api/agent/nodes/{id}/state", h.agentState)
	r.Post("/api/agent/nodes/{id}/ack", h.agentAck)
}
