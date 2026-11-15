package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dementia/backend/internal/api/httpx"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handler) setupStatus(w http.ResponseWriter, r *http.Request) {
	configured, err := h.store.AdminConfigured(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]bool{
		"setup_required":  !configured,
		"setup_available": !configured,
	})
}

func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	configured, err := h.store.AdminConfigured(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	if configured {
		httpx.Failure(w, http.StatusConflict, "administrator is already configured")
		return
	}
	var req credentials
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(req.Username) || len([]rune(req.Password)) < 12 || len(req.Password) > 1024 {
		httpx.Failure(w, http.StatusBadRequest, "invalid username or password length")
		return
	}
	if !h.acquirePasswordSlot(w) {
		return
	}
	defer func() { <-h.passwordSlots }()
	salt := make([]byte, 16)
	rand.Read(salt)
	var pair tokenPair
	issue := func(id string) (string, time.Time, error) {
		var err error
		pair, err = h.issuePair(id)
		return pair.refresh, pair.refreshExpires(), err
	}
	if err := h.store.SetupAdmin(r.Context(), req.Username, salt, passwordHash(req.Password, salt), issue); err != nil {
		httpx.HandleError(w, err)
		return
	}
	pair.writeCookies(w)
	httpx.Respond(w, http.StatusCreated, map[string]string{"username": req.Username})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(req.Username) || len(req.Password) == 0 || len(req.Password) > 1024 {
		httpx.Failure(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !h.acquirePasswordSlot(w) {
		return
	}
	defer func() { <-h.passwordSlots }()
	if !h.allowLogin(w, r) {
		return
	}
	salt, hash, err := h.store.AdminCredentials(r.Context(), req.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpx.HandleError(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		salt, hash = make([]byte, 16), make([]byte, 32)
	}
	matches := subtle.ConstantTimeCompare(passwordHash(req.Password, salt), hash) == 1
	if !matches || err != nil {
		httpx.Failure(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	var pair tokenPair
	issue := func(id string) (string, time.Time, error) {
		var err error
		pair, err = h.issuePair(id)
		return pair.refresh, pair.refreshExpires(), err
	}
	if err := h.store.CreateAdminSession(r.Context(), req.Username, salt, hash, issue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Failure(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		httpx.HandleError(w, err)
		return
	}
	pair.writeCookies(w)
	httpx.Respond(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	if username, ok := h.authenticate(w, r); ok {
		required, err := h.store.AdminNeedsPasswordChange(r.Context(), username)
		if err != nil {
			httpx.HandleError(w, err)
			return
		}
		httpx.Respond(w, http.StatusOK, map[string]any{"username": username, "password_change_required": required})
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	id, err := h.parseToken(readToken(r, accessCookie), "access")
	if err == nil {
		err = h.store.DeleteAdminSession(r.Context(), id)
	} else if refresh := readToken(r, refreshCookie); refresh != "" {
		err = h.store.DeleteAdminSessionByRefresh(r.Context(), refresh)
	} else {
		err = nil
	}
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	setTokenCookie(w, accessCookie, "", time.Unix(0, 0), -1)
	setTokenCookie(w, refreshCookie, "", time.Unix(0, 0), -1)
	httpx.Respond(w, http.StatusNoContent, nil)
}

func passwordHash(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
}

func (h *Handler) acquirePasswordSlot(w http.ResponseWriter) bool {
	select {
	case h.passwordSlots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		httpx.Failure(w, http.StatusTooManyRequests, "too many authentication requests")
		return false
	}
}

func (h *Handler) allowLogin(w http.ResponseWriter, r *http.Request) bool {
	if h.limiter != nil {
		allowed, err := h.limiter.Allow(r.Context())
		if err != nil {
			httpx.Failure(w, http.StatusServiceUnavailable, "login temporarily unavailable")
			return false
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			httpx.Failure(w, http.StatusTooManyRequests, "too many login attempts")
		}
		return allowed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !now.Before(h.windowEnd) {
		h.windowEnd = now.Add(time.Minute)
		h.loginAttempts = 0
	}
	if h.loginAttempts >= 20 {
		w.Header().Set("Retry-After", "60")
		httpx.Failure(w, http.StatusTooManyRequests, "too many login attempts")
		return false
	}
	h.loginAttempts++
	return true
}

// completeOnboarding is available only to an authenticated administrator with a temporary credential.
func (h *Handler) completeOnboarding(w http.ResponseWriter, r *http.Request) {
	username, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	required, err := h.store.AdminNeedsPasswordChange(r.Context(), username)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	if !required {
		httpx.Failure(w, http.StatusConflict, "password setup is already complete")
		return
	}
	var req credentials
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(req.Username) || len([]rune(req.Password)) < 12 || len(req.Password) > 1024 {
		httpx.Failure(w, http.StatusBadRequest, "invalid username or password length")
		return
	}
	if !h.acquirePasswordSlot(w) {
		return
	}
	defer func() { <-h.passwordSlots }()
	salt := make([]byte, 16)
	rand.Read(salt)
	var pair tokenPair
	issue := func(id string) (string, time.Time, error) {
		var err error
		pair, err = h.issuePair(id)
		return pair.refresh, pair.refreshExpires(), err
	}
	if err := h.store.CompleteAdminOnboarding(r.Context(), username, req.Username, salt, passwordHash(req.Password, salt), issue); err != nil {
		httpx.HandleError(w, err)
		return
	}
	pair.writeCookies(w)
	httpx.Respond(w, http.StatusOK, map[string]any{"username": req.Username, "password_change_required": false})
}
