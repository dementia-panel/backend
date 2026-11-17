package health

import (
	"context"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func check(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/health/check"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	httpx.Respond(w, http.StatusOK, map[string]string{"status": "ok"})
}
func readiness(ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			httpx.Failure(w, http.StatusServiceUnavailable, "service dependencies unavailable")
			return
		}
		check(w, r)
	}
}
