package agent

import (
	"context"
	"errors"
	"net/http"
	"time"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) agentState(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	state, err := h.store.State(r.Context(), id, httpx.Bearer(r))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Failure(w, 403, "invalid agent credentials")
		return
	}
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, 200, state)
}

func (h *Handler) agentAck(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	token := httpx.Bearer(r)
	if len(token) != 43 {
		httpx.Failure(w, http.StatusForbidden, "invalid agent credentials")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	valid, err := h.store.AgentExists(ctx, id, token)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	if !valid {
		httpx.Failure(w, http.StatusForbidden, "invalid agent credentials")
		return
	}
	select {
	case h.reports <- struct{}{}:
		defer func() { <-h.reports }()
	default:
		w.Header().Set("Retry-After", "1")
		httpx.Failure(w, http.StatusTooManyRequests, "too many concurrent agent reports")
		return
	}
	var req struct {
		Revision   int64                           `json:"revision"`
		Handshakes map[string]int64                `json:"handshakes"`
		Traffic    map[string]store.TrafficCounter `json:"traffic"`
		Restarted  int64                           `json:"restarted_revision"`
	}
	// Allow handshake and transfer reports for every peer in the /16 pool.
	r.Body = http.MaxBytesReader(w, r.Body, 14<<20)
	if !httpx.DecodeBody(w, r, &req) {
		return
	}
	if req.Revision < 1 || req.Restarted < 0 {
		httpx.Failure(w, 400, "invalid revision")
		return
	}
	if err := h.store.Ack(ctx, id, token, req.Revision, req.Restarted, req.Handshakes, req.Traffic); err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, 200, map[string]bool{"ok": true})
}
