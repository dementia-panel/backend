package logging

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func Configure(raw string) error {
	level := slog.LevelInfo
	if raw != "" && level.UnmarshalText([]byte(raw)) != nil {
		return errors.New("invalid LOG_LEVEL")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	return nil
}

func Requests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(writer, r)
		status := writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		route := chi.RouteContext(r.Context()).RoutePattern()
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status == 429:
			level = slog.LevelWarn
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz" ||
			(status < 400 && strings.HasPrefix(route, "/api/agent/")):
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request", "method", r.Method,
			"route", route, "status", status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", writer.BytesWritten())
	})
}

// RecoverRequests keeps panic logs free of request URLs, headers, and panic values.
func RecoverRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				slog.ErrorContext(r.Context(), "HTTP handler panicked", "route",
					chi.RouteContext(r.Context()).RoutePattern(), "stack", string(debug.Stack()))
				if writer, ok := w.(middleware.WrapResponseWriter); ok && writer.Status() != 0 {
					panic(http.ErrAbortHandler)
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
