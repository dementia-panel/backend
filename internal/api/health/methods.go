package health

import (
	"context"
	"net/http"
	"time"

	"dementia/backend/internal/api/httpx"
)

func check(w http.ResponseWriter, r *http.Request) {
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
