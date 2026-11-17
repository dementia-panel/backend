package auth

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
)

const (
	accessCookie    = "__Host-dementia-access"
	refreshCookie   = "__Host-dementia-refresh"
	accessLifetime  = 15 * time.Minute
	refreshLifetime = 4 * time.Hour
	tokenIssuer     = "dementia"
)

type tokenPair struct {
	access, refresh string
	issuedAt        time.Time
}

func (h *Handler) issuePair(sessionID string) (tokenPair, error) {
	now := time.Now().UTC().Truncate(time.Second)
	pair := tokenPair{issuedAt: now}
	var err error
	pair.access, err = h.signToken(sessionID, "access", now, accessLifetime)
	if err != nil {
		return tokenPair{}, err
	}
	pair.refresh, err = h.signToken(sessionID, "refresh", now, refreshLifetime)
	return pair, err
}
func (h *Handler) signToken(sessionID, audience string, now time.Time, ttl time.Duration) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: tokenIssuer, Subject: sessionID, Audience: jwt.ClaimStrings{audience}, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl)), ID: store.NewToken()}).SignedString(h.signingKey)
}
func (h *Handler) parseToken(raw, audience string) (string, error) {
	claims := new(jwt.RegisteredClaims)
	_, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return h.signingKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuer(tokenIssuer), jwt.WithAudience(audience), jwt.WithIssuedAt(), jwt.WithNotBeforeRequired())
	if claims.Issuer != tokenIssuer || claims.IssuedAt == nil || claims.NotBefore == nil || !httpx.ValidID(claims.Subject) || len(claims.Audience) != 1 || claims.Audience[0] != audience || claims.ID == "" || errors.Is(err, jwt.ErrTokenNotValidYet) || errors.Is(err, jwt.ErrTokenUsedBeforeIssued) {
		return "", errors.New("invalid token claims")
	}
	return claims.Subject, err
}
func readToken(r *http.Request, name string) string {
	const op = "internal/api/auth/readToken"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
func (p tokenPair) refreshExpires() time.Time {
	return p.issuedAt.Add(refreshLifetime)
}
func (p tokenPair) writeCookies(w http.ResponseWriter) {
	setTokenCookie(w, accessCookie, p.access, p.issuedAt.Add(accessLifetime), int(accessLifetime.Seconds()))
	setTokenCookie(w, refreshCookie, p.refresh, p.refreshExpires(), int(refreshLifetime.Seconds()))
}
func setTokenCookie(w http.ResponseWriter, name, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
