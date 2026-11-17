package auth

import (
	"context"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

type LoginLimiter interface {
	Allow(context.Context) (bool, error)
}
type Handler struct {
	store         store.Store
	origin        string
	signingKey    []byte
	limiter       LoginLimiter
	mu            sync.Mutex
	windowEnd     time.Time
	loginAttempts int
	passwordSlots chan struct{}
}

func New(repo store.Store, origin string, signingKey []byte, limiter LoginLimiter) *Handler {
	return &Handler{store: repo, origin: origin, signingKey: signingKey, limiter: limiter, passwordSlots: make(chan struct{}, 2)}
}
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/auth", func(r chi.Router) {
		r.Use(h.checkOrigin)
		r.Get("/setup", h.setupStatus)
		r.Post("/setup", h.setup)
		r.Post("/login", h.login)
		r.Get("/me", h.me)
		r.Post("/logout", h.logout)
		r.Post("/complete-setup", h.completeOnboarding)
	})
}
func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return h.checkOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if username, ok := h.authenticate(w, r); ok {
			required, err := h.store.AdminNeedsPasswordChange(r.Context(), middleware.GetReqID(r.Context()), username)
			if err != nil {
				httpx.HandleError(w, r, err)
				return
			}
			if required {
				httpx.Failure(w, http.StatusForbidden, "password setup required")
				return
			}
			next.ServeHTTP(w, r)
		}
	}))
}
func (h *Handler) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && origin != "" && origin != h.origin {
			httpx.Failure(w, http.StatusForbidden, "invalid origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	const op = "internal/api/auth/authenticate"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	access := readToken(r, accessCookie)
	var accessSession string
	if access != "" {
		id, err := h.parseToken(access, "access")
		if err == nil {
			return h.lookupSession(w, r, id)
		}
		if !errors.Is(err, jwt.ErrTokenExpired) {
			httpx.Failure(w, http.StatusUnauthorized, "invalid access token")
			return "", false
		}
		accessSession = id
	}
	refresh := readToken(r, refreshCookie)
	id, err := h.parseToken(refresh, "refresh")
	if err != nil || (accessSession != "" && accessSession != id) {
		httpx.Failure(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	username, ok := h.lookupSession(w, r, id)
	if !ok {
		return "", false
	}
	pair, err := h.issuePair(id)
	if err != nil {
		httpx.HandleError(w, r, err)
		return "", false
	}
	rotated, err := h.store.RotateAdminSession(r.Context(), requestID, id, refresh, pair.refresh, pair.refreshExpires())
	if err != nil {
		httpx.HandleError(w, r, err)
		return "", false
	}
	if !rotated {
		httpx.Failure(w, http.StatusConflict, "refresh token is no longer current")
		return "", false
	}
	pair.writeCookies(w)
	return username, true
}
func (h *Handler) lookupSession(w http.ResponseWriter, r *http.Request, id string) (string, bool) {
	const op = "internal/api/auth/lookupSession"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	username, err := h.store.AdminSession(r.Context(), requestID, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Failure(w, http.StatusUnauthorized, "authentication required")
	case err != nil:
		httpx.HandleError(w, r, err)
	default:
		return username, true
	}
	return "", false
}
