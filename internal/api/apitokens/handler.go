package apitokens

import (
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const tokenPrefix = "dementia_api_"
const legacyTokenPrefix = "amz_api_"
const previousTokenPrefix = "relay_api_"

type Handler struct{ store store.Store }

func Register(r chi.Router, repo store.Store) {
	h := &Handler{store: repo}
	r.Get("/api-tokens", h.list)
	r.Post("/api-tokens", h.create)
	r.Delete("/api-tokens/{id}", h.delete)
	r.Patch("/api-tokens/{id}", h.rename)
}
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
			validFormat := (strings.HasPrefix(token, tokenPrefix) && len(token) == len(tokenPrefix)+43) || (strings.HasPrefix(token, legacyTokenPrefix) && len(token) == len(legacyTokenPrefix)+43) || (strings.HasPrefix(token, previousTokenPrefix) && len(token) == len(previousTokenPrefix)+43)
			if len(authorization) != 1 || !validFormat {
				httpx.Failure(w, r, http.StatusUnauthorized, "invalid API token")
				return
			}
			valid, err := repo.APITokenExists(r.Context(), middleware.GetReqID(r.Context()), token)
			if err != nil {
				httpx.HandleError(w, r, err)
				return
			}
			if !valid {
				httpx.Failure(w, r, http.StatusUnauthorized, "invalid API token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
