package apitokens

import (
	"net/http"
	"strings"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

const tokenPrefix = "dementia_api_"

// Registered keys issued before the rename remain valid until revoked.
const legacyTokenPrefix = "amz_api_"
const previousTokenPrefix = "relay_api_"

type Handler struct {
	store store.Store
}

func Register(r chi.Router, repo store.Store) {
	h := &Handler{store: repo}
	r.Get("/api-tokens", h.list)
	r.Post("/api-tokens", h.create)
	r.Delete("/api-tokens/{id}", h.delete)
}

// AllowBearer authenticates requests with an Authorization header using an API token.
func AllowBearer(repo store.Store, requireSession func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		session := requireSession(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := r.Header.Values("Authorization")
			if len(authorization) == 0 {
				session.ServeHTTP(w, r)
				return
			}
			token := httpx.Bearer(r)
			validFormat := (strings.HasPrefix(token, tokenPrefix) && len(token) == len(tokenPrefix)+43) ||
				(strings.HasPrefix(token, legacyTokenPrefix) && len(token) == len(legacyTokenPrefix)+43) ||
				(strings.HasPrefix(token, previousTokenPrefix) && len(token) == len(previousTokenPrefix)+43)
			if len(authorization) != 1 || !validFormat {
				httpx.Failure(w, http.StatusUnauthorized, "invalid API token")
				return
			}
			valid, err := repo.APITokenExists(r.Context(), token)
			if err != nil {
				httpx.HandleError(w, err)
				return
			}
			if !valid {
				httpx.Failure(w, http.StatusUnauthorized, "invalid API token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
