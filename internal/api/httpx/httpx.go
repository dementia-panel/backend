package httpx

import (
	"context"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func Bearer(r *http.Request) string {
	const op = "internal/api/httpx/Bearer"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return token
}
func Respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func Failure(w http.ResponseWriter, r *http.Request, status int, message string) {
	const op = "internal/api/httpx/Failure"
	log := with.WithOpAndRequestID(slog.Default(), op, middleware.GetReqID(r.Context()))
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	route := ""
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		route = routeContext.RoutePattern()
	}
	log.Log(r.Context(), level, "HTTP request rejected", "method", r.Method, "route", route, "status", status, sl.Err(errors.New(message)))
	Respond(w, status, map[string]string{"error": message})
}
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	const op = "internal/api/httpx/HandleError"
	log := with.WithOpAndRequestID(slog.Default(), op, middleware.GetReqID(r.Context()))
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		Failure(w, r, 404, "resource not found")
	case errors.Is(err, store.ErrDeviceLimit), errors.Is(err, store.ErrAmbiguousUser), errors.Is(err, store.ErrNodeUnavailable), errors.Is(err, store.ErrAddressPoolFull):
		Failure(w, r, 409, err.Error())
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "57014" || pgErr.Code == "40P01" || pgErr.Code == "40001"):
		Failure(w, r, http.StatusServiceUnavailable, "database operation temporarily unavailable")
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		Failure(w, r, 409, "resource already exists")
	default:
		log.ErrorContext(r.Context(), "request failed", sl.Err(err))
		Failure(w, r, 500, "internal error")
	}
}
func ErrorType(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return "backend"
}
func Decode(w http.ResponseWriter, r *http.Request, value any) bool {
	const op = "internal/api/httpx/Decode"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return DecodeBody(w, r, value)
}
func DecodeBody(w http.ResponseWriter, r *http.Request, value any) bool {
	const op = "internal/api/httpx/DecodeBody"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		Failure(w, r, 400, "invalid JSON")
		return false
	}
	return true
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func IDParam(w http.ResponseWriter, r *http.Request) string {
	const op = "internal/api/httpx/IDParam"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := chi.URLParam(r, "id")
	if !ValidID(id) {
		Failure(w, r, 400, "invalid ID")
		return ""
	}
	return id
}
func ValidID(id string) bool {
	return uuidPattern.MatchString(id)
}
func UserIDParam(w http.ResponseWriter, r *http.Request, repo store.Store) string {
	const op = "internal/api/httpx/UserIDParam"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	return UserReference(w, r, repo, chi.URLParam(r, "id"))
}
func UserReference(w http.ResponseWriter, r *http.Request, repo store.Store, value string) string {
	const op = "internal/api/httpx/UserReference"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	var id string
	var err error
	if ValidID(value) {
		err = repo.DB.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1`, value).Scan(&id)
	} else {
		telegram, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || telegram <= 0 {
			Failure(w, r, http.StatusBadRequest, "user must be identified by UUID or positive telegram_id")
			return ""
		}
		id, err = repo.UserIDByTelegram(r.Context(), requestID, telegram)
	}
	if err != nil {
		HandleError(w, r, err)
		return ""
	}
	return id
}
