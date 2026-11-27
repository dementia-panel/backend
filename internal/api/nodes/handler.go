package nodes

import (
	"context"
	"dementia/backend/internal/jobs"
	"dementia/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store        store.Store
	publicURL    string
	context      context.Context
	workers      *jobs.Group
	installSlots chan struct{}
}

func Register(r chi.Router, repo store.Store, publicURL string, ctx context.Context, workers *jobs.Group) {
	h := &Handler{store: repo, publicURL: publicURL, context: ctx, workers: workers, installSlots: make(chan struct{}, 4)}
	r.Get("/nodes", h.nodes)
	r.Get("/nodes/metrics", h.metrics)
	r.Get("/nodes/{id}", h.node)
	r.Post("/nodes", h.createNode)
	r.Patch("/nodes/{id}", h.updateNode)
	r.Put("/nodes/{id}/endpoint", h.setEndpoint)
	r.Post("/nodes/{id}/actions/check", h.checkNode)
	r.Post("/nodes/{id}/actions/restart", h.restartNode)
	r.Delete("/nodes/{id}", h.retireNode)
	r.Post("/nodes/actions/restart-all", h.restartAllNodes)
}

func RegisterUpgrade(r chi.Router, repo store.Store, ctx context.Context, workers *jobs.Group) {
	h := &Handler{store: repo, context: ctx, workers: workers, installSlots: make(chan struct{}, 4)}
	r.Post("/nodes/{id}/actions/upgrade", h.upgradeNode)
}
