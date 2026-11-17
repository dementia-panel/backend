package health

import (
	"context"

	"github.com/go-chi/chi/v5"
)

func Register(r chi.Router, ready func(context.Context) error) {
	r.Get("/healthz", check)
	if ready != nil {
		r.Get("/readyz", readiness(ready))
	}
}
