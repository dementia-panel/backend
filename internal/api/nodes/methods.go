package nodes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/provision"
	"dementia/backend/internal/store"
	"github.com/jackc/pgx/v5"
)

var loginPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)

func (h *Handler) nodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.store.Nodes(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, 200, nodes)
}

func (h *Handler) node(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	node, err := h.store.Node(r.Context(), id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, node)
}

func (h *Handler) createNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Login    string `json:"login"`
		Password string `json:"password"`
		SSHPort  int    `json:"ssh_port"`
		VPNPort  int    `json:"vpn_port"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	ip, ipErr := netip.ParseAddr(req.IP)
	req.Name = strings.TrimSpace(req.Name)
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.VPNPort == 0 {
		req.VPNPort = 51820
	}
	if ipErr != nil || ip.Zone() != "" || !ip.IsGlobalUnicast() || !validNodeName(req.Name) ||
		!loginPattern.MatchString(req.Login) || req.Password == "" || len(req.Password) > 4096 ||
		strings.ContainsAny(req.Password, "\r\n\x00") ||
		req.SSHPort < 1 || req.SSHPort > 65535 || req.VPNPort < 1 || req.VPNPort > 65535 {
		httpx.Failure(w, 400, "invalid node settings")
		return
	}
	select {
	case h.installSlots <- struct{}{}:
	default:
		httpx.Failure(w, 429, "all installation workers are busy")
		return
	}
	token := store.NewToken()
	n, err := h.store.ReserveNode(r.Context(), req.Name, ip.Unmap().String(), token, req.SSHPort, req.VPNPort)
	if err != nil {
		<-h.installSlots
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Failure(w, 409, "node already registered")
			return
		}
		httpx.HandleError(w, err)
		return
	}
	input := provision.Request{ID: n.ID, Name: n.Name, IP: n.IP, Login: req.Login, Password: req.Password, Token: token, PanelURL: h.publicURL, SSHPort: n.SSHPort, VPNPort: n.VPNPort}
	input.VerifyHostKey = func(ctx context.Context, key string) error { return h.store.TrustNodeHostKey(ctx, n.ID, token, key) }
	started := h.workers.Start("node installation", func() {
		defer func() { <-h.installSlots }()
		reason := "node installation interrupted"
		var public, header string
		defer func() {
			resultCtx, cancelResult := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelResult()
			if err := h.store.FinishInstall(resultCtx, n.ID, token, public, header, reason); err != nil {
				slog.Error("failed to save node installation result", "node_id", n.ID, "type", httpx.ErrorType(err))
			}
		}()
		ctx, cancel := context.WithTimeout(h.context, 15*time.Minute)
		defer cancel()
		input.Progress = func(stage string) {
			progressCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_ = h.store.InstallStage(progressCtx, n.ID, token, stage)
		}
		var installErr error
		public, header, installErr = provision.Install(ctx, input)
		input.Password = ""
		if installErr != nil {
			reason = installErr.Error()
		} else {
			reason = ""
		}
	})
	if !started {
		<-h.installSlots
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.store.FinishInstall(ctx, n.ID, token, "", "", "backend is shutting down"); err != nil {
			slog.Error("failed to cancel node installation", "node_id", n.ID)
		}
		httpx.Failure(w, http.StatusServiceUnavailable, "backend is shutting down")
		return
	}
	httpx.Respond(w, 202, n)
}

func validNodeName(name string) bool {
	return name != "" && len(name) <= 128 && !strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	})
}

func (h *Handler) updateNode(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Name == nil && req.Enabled == nil {
		httpx.Failure(w, 400, "provide name or enabled")
		return
	}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		if !validNodeName(*req.Name) {
			httpx.Failure(w, 400, "invalid node name")
			return
		}
	}
	n, err := h.store.UpdateNode(r.Context(), id, req.Name, req.Enabled)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, 200, n)
}

func (h *Handler) restartNode(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	n, err := h.store.RestartNode(r.Context(), id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, n)
}

func (h *Handler) checkNode(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	ctx := r.Context()
	n, err := h.store.Node(ctx, id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	previous := n.LastSeen
	responded := func() bool {
		return n.LastSeen != nil && (previous == nil || n.LastSeen.After(*previous))
	}
	if n.Status == "ready" {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timeout := time.NewTimer(12 * time.Second)
		defer timeout.Stop()
	poll:
		for !responded() && n.Status == "ready" {
			select {
			case <-ctx.Done():
				return
			case <-timeout.C:
				break poll
			case <-ticker.C:
				n, err = h.store.Node(ctx, id)
				if err != nil {
					httpx.HandleError(w, err)
					return
				}
			}
		}
	}
	httpx.Respond(w, 200, struct {
		store.Node
		Responded bool `json:"responded"`
	}{n, responded()})
}

func (h *Handler) retireNode(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !loginPattern.MatchString(req.Login) || req.Password == "" || len(req.Password) > 4096 || strings.ContainsAny(req.Password, "\r\n\x00") {
		httpx.Failure(w, http.StatusBadRequest, "invalid SSH credentials")
		return
	}
	select {
	case h.installSlots <- struct{}{}:
	default:
		httpx.Failure(w, http.StatusTooManyRequests, "all installation workers are busy")
		return
	}
	node, err := h.store.BeginNodeRemoval(r.Context(), id)
	if err != nil {
		<-h.installSlots
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Failure(w, http.StatusConflict, "node is unavailable or busy")
			return
		}
		httpx.HandleError(w, err)
		return
	}
	input := provision.Request{ID: id, IP: node.IP, SSHPort: node.SSHPort, Login: req.Login, Password: req.Password}
	input.VerifyHostKey = func(ctx context.Context, key string) error { return h.store.VerifyRemovalHostKey(ctx, id, key) }
	finish := func(reason string) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := h.store.FinishNodeRemoval(ctx, id, reason); err != nil {
			slog.Error("failed to save node removal result", "node_id", id, "type", httpx.ErrorType(err))
			if reason == "" {
				_ = h.store.FinishNodeRemoval(ctx, id, "VPS cleaned but panel removal failed; retry removal")
			}
		}
	}
	if !h.workers.Start("node removal", func() {
		defer func() { <-h.installSlots }()
		reason := "remote cleanup interrupted; retry removal"
		defer func() { finish(reason) }()
		ctx, cancel := context.WithTimeout(h.context, 3*time.Minute)
		defer cancel()
		if err := provision.Remove(ctx, input); err != nil {
			reason = err.Error()
		} else {
			reason = ""
		}
		input.Password = ""
	}) {
		<-h.installSlots
		finish("backend is shutting down")
		httpx.Failure(w, http.StatusServiceUnavailable, "backend is shutting down")
		return
	}
	httpx.Respond(w, 202, node)
}

func (h *Handler) restartAllNodes(w http.ResponseWriter, r *http.Request) {
	count, err := h.store.RestartAllNodes(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, map[string]int64{"queued": count})
}

var endpointLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (h *Handler) setEndpoint(w http.ResponseWriter, r *http.Request) {
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	var req struct {
		Host *string `json:"host"`
		Port *int    `json:"port"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Host != nil {
		host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*req.Host), "."))
		if !validEndpoint(host) {
			httpx.Failure(w, http.StatusBadRequest, "host must be an IP address or DNS name")
			return
		}
		req.Host = &host
	}
	if req.Port != nil && (*req.Port < 1 || *req.Port > 65535) {
		httpx.Failure(w, http.StatusBadRequest, "invalid endpoint port")
		return
	}
	node, err := h.store.SetNodeEndpoint(r.Context(), id, req.Host, req.Port)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	httpx.Respond(w, http.StatusOK, node)
}

func validEndpoint(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == "" && ip.IsGlobalUnicast()
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !endpointLabel.MatchString(label) {
			return false
		}
	}
	return true
}
