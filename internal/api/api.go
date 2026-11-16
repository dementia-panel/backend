package api

import (
	"context"
	"net/http"
	"time"

	"dementia/backend/internal/api/agent"
	"dementia/backend/internal/api/apitokens"
	"dementia/backend/internal/api/auth"
	"dementia/backend/internal/api/health"
	"dementia/backend/internal/api/nodes"
	"dementia/backend/internal/api/publicsubscription"
	"dementia/backend/internal/api/settings"
	"dementia/backend/internal/api/subscriptions"
	"dementia/backend/internal/api/traffic"
	"dementia/backend/internal/api/users"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/jobs"
	"dementia/backend/internal/logging"
	"dementia/backend/internal/store"
	"dementia/backend/internal/telegram"
	"github.com/go-chi/chi/v5"
)

type API struct {
	IntegrationStatus func() integration.Snapshot
	Store             store.Store
	JWTSigningKey     []byte
	LoginLimiter      auth.LoginLimiter
	Ready             func(context.Context) error
	TelegramWebhook   http.HandlerFunc
	PublicURL         string
	Context           context.Context
	Workers           jobs.Group
}

func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(logging.Requests, logging.RecoverRequests)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	health.Register(r, a.Ready)
	if a.TelegramWebhook != nil {
		r.Post(telegram.WebhookPath, a.TelegramWebhook)
	}
	admin := auth.New(a.Store, a.PublicURL, a.JWTSigningKey, a.LoginLimiter)
	admin.Register(r)
	r.Route("/api/admin", func(r chi.Router) {
		apitokens.Register(r.With(admin.RequireSession), a.Store)
		settings.Register(r.With(admin.RequireSession), a.Store, a.IntegrationStatus)
		subscriptions.RegisterProfileExport(r.With(admin.RequireSession), a.Store)
		r.Group(func(r chi.Router) {
			r.Use(apitokens.AllowBearer(a.Store, admin.RequireSession))
			nodes.Register(r, a.Store, a.PublicURL, a.Context, &a.Workers)
			users.Register(r, a.Store, a.PublicURL)
			subscriptions.Register(r, a.Store, a.PublicURL)
			traffic.Register(r, a.Store)
		})
	})
	publicsubscription.Register(r, a.Store, a.PublicURL)
	agent.Register(r, a.Store)
	return r
}
