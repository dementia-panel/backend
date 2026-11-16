package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func Bearer(r *http.Request) string {
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

func Failure(w http.ResponseWriter, status int, message string) {
	Respond(w, status, map[string]string{"error": message})
}

func HandleError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		Failure(w, 404, "resource not found")
	case errors.Is(err, store.ErrDeviceLimit), errors.Is(err, store.ErrAmbiguousUser), errors.Is(err, store.ErrNodeUnavailable), errors.Is(err, store.ErrAddressPoolFull):
		Failure(w, 409, err.Error())
	case errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "57014" || pgErr.Code == "40P01" || pgErr.Code == "40001"):
		Failure(w, http.StatusServiceUnavailable, "database operation temporarily unavailable")
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		Failure(w, 409, "resource already exists")
	default:
		slog.Error("request failed", "type", ErrorType(err))
		Failure(w, 500, "internal error")
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return DecodeBody(w, r, value)
}

func DecodeBody(w http.ResponseWriter, r *http.Request, value any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		Failure(w, 400, "invalid JSON")
		return false
	}
	return true
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func IDParam(w http.ResponseWriter, r *http.Request) string {
	id := chi.URLParam(r, "id")
	if !ValidID(id) {
		Failure(w, 400, "invalid ID")
		return ""
	}
	return id
}

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

// UserIDParam accepts a UUID or a positive Telegram ID in user routes.
func UserIDParam(w http.ResponseWriter, r *http.Request, repo store.Store) string {
	return UserReference(w, r, repo, chi.URLParam(r, "id"))
}

func UserReference(w http.ResponseWriter, r *http.Request, repo store.Store, value string) string {
	var id string
	var err error
	if ValidID(value) {
		err = repo.DB.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1`, value).Scan(&id)
	} else {
		telegram, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || telegram <= 0 {
			Failure(w, http.StatusBadRequest, "user must be identified by UUID or positive telegram_id")
			return ""
		}
		id, err = repo.UserIDByTelegram(r.Context(), telegram)
	}
	if err != nil {
		HandleError(w, err)
		return ""
	}
	return id
}
