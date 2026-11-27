package agent

import (
	"context"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
)

var nodeVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func validNodeVersion(value string) bool {
	return nodeVersionPattern.MatchString(value)
}

func (h *Handler) agentState(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/agent/agentState"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	state, err := h.store.State(r.Context(), requestID, id, httpx.Bearer(r))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Failure(w, r, 403, "invalid agent credentials")
		return
	}
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, state)
}
func (h *Handler) agentAck(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/agent/agentAck"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	token := httpx.Bearer(r)
	if len(token) != 43 {
		httpx.Failure(w, r, http.StatusForbidden, "invalid agent credentials")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	valid, err := h.store.AgentExists(ctx, requestID, id, token)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	if !valid {
		httpx.Failure(w, r, http.StatusForbidden, "invalid agent credentials")
		return
	}
	select {
	case h.reports <- struct{}{}:
		defer func() {
			<-h.reports
		}()
	default:
		w.Header().Set("Retry-After", "1")
		httpx.Failure(w, r, http.StatusTooManyRequests, "too many concurrent agent reports")
		return
	}
	var req struct {
		Revision    int64                           `json:"revision"`
		Handshakes  map[string]int64                `json:"handshakes"`
		Traffic     map[string]store.TrafficCounter `json:"traffic"`
		Restarted   int64                           `json:"restarted_revision"`
		NodeVersion string                          `json:"node_version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 14<<20)
	if !httpx.DecodeBody(w, r, &req) {
		return
	}
	if req.Revision < 1 || req.Restarted < 0 {
		httpx.Failure(w, r, 400, "invalid revision")
		return
	}
	if len(req.NodeVersion) > 32 || (req.NodeVersion != "" && !validNodeVersion(req.NodeVersion)) {
		httpx.Failure(w, r, 400, "invalid node version")
		return
	}
	if err := h.store.Ack(ctx, requestID, id, token, req.Revision, req.Restarted, req.Handshakes, req.Traffic, req.NodeVersion); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]bool{"ok": true})
}
