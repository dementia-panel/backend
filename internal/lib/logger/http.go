package logger

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

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
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || (status < 400 && strings.HasPrefix(route, "/api/agent/")):
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request", "method", r.Method, "request_id", middleware.GetReqID(r.Context()), "route", route, "status", status, "duration_ms", time.Since(start).Milliseconds(), "bytes", writer.BytesWritten())
	})
}
func RecoverRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				slog.ErrorContext(r.Context(), "HTTP handler panicked", "request_id", middleware.GetReqID(r.Context()), "route", chi.RouteContext(r.Context()).RoutePattern(), "stack", string(debug.Stack()))
				if writer, ok := w.(middleware.WrapResponseWriter); ok && writer.Status() != 0 {
					panic(http.ErrAbortHandler)
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
