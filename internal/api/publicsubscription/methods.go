package publicsubscription

import (
	"archive/zip"
	"bytes"
	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/lib/logger/with"
	"dementia/backend/internal/store"
	"log/slog"

	_ "embed"
	"html/template"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type download struct {
	store.Location
	URL        string `json:"url,omitempty"`
	AmneziaURL string `json:"amnezia_url,omitempty"`
	KeyURL     string `json:"key_url,omitempty"`
}
type publicDevice struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Online          bool                 `json:"online"`
	LastConnectedAt *time.Time           `json:"last_connected_at"`
	Servers         []store.DeviceServer `json:"servers"`
}
type page struct {
	DeviceOptions []store.DeviceOption        `json:"device_options"`
	Devices       []publicDevice              `json:"devices"`
	Name          string                      `json:"name"`
	ExpiresAt     *time.Time                  `json:"expires_at"`
	Locations     []download                  `json:"locations"`
	Branding      *store.SubscriptionBranding `json:"branding,omitempty"`
}

func (h *Handler) publicPage(r *http.Request) (page, error) {
	const op = "internal/api/publicsubscription/publicPage"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	token := chi.URLParam(r, "token")
	sub, locations, err := h.store.PublicSubscription(r.Context(), requestID, token)
	if err != nil {
		return page{}, err
	}
	records, err := h.store.PublicDevices(r.Context(), requestID, sub.ID, token)
	if err != nil {
		return page{}, err
	}
	servers, err := h.store.PublicDeviceServers(r.Context(), requestID, sub.ID, token)
	if err != nil {
		return page{}, err
	}
	byDevice := make(map[string][]store.DeviceServer)
	for _, server := range servers {
		byDevice[server.DeviceID] = append(byDevice[server.DeviceID], server)
	}
	devices := make([]publicDevice, 0, len(records))
	for _, d := range records {
		devices = append(devices, publicDevice{ID: d.ID, Name: d.Name, Online: d.Online, LastConnectedAt: d.LastConnectedAt, Servers: byDevice[d.ID]})
	}
	options, err := h.store.DeviceOptions(r.Context(), requestID, sub.UserID)
	if err != nil {
		return page{}, err
	}
	result := page{DeviceOptions: options, Devices: devices, Name: sub.Name, ExpiresAt: sub.ExpiresAt, Locations: make([]download, 0, len(locations))}
	for _, l := range locations {
		d := download{Location: l}
		if len(devices) == 0 {
			d.Available = false
		}
		if d.Available {
			nodeURL := h.publicURL + "/s/" + token + "/nodes/" + l.ID
			d.URL = nodeURL + ".conf"
			d.AmneziaURL = nodeURL + ".vpn"
			d.KeyURL = nodeURL + ".key"
		}
		result.Locations = append(result.Locations, d)
	}
	return result, nil
}

var subscriptionTemplate = template.Must(template.New("subscription").Parse(`<!doctype html>
<html lang="ru">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}} · Dementia</title>
<script src="/s/assets/copy-key.js" defer></script>
<main>
<h1>{{.Name}}</h1>
{{if .ExpiresAt}}<p>Действует до {{.ExpiresAt.UTC.Format "02.01.2006 15:04"}} UTC.</p>{{end}}
<p>Выберите сервер. В приложении AmneziaVPN можно импортировать файл .vpn или вставить ключ vpn://.</p>
<ul>{{range .Locations}}<li>
<strong>{{.Name}}</strong>
{{if .URL}}
<a href="{{.URL}}">Скачать .conf</a> ·
<a href="{{.AmneziaURL}}">Скачать .vpn</a> ·
<a href="{{.KeyURL}}" data-copy-key>Скопировать vpn://</a>
{{else}}— профиль пока недоступен{{end}}
</li>{{else}}<li>В подписке пока нет серверов.</li>{{end}}</ul>
<p id="copy-status" role="status"></p>
<textarea id="copy-fallback" aria-label="Ключ vpn://" readonly hidden></textarea>
</main>
</html>`))

//go:embed copy-key.js
var copyKeyJS string

func copyKeyScript(w http.ResponseWriter, _ *http.Request) {
	publicHeaders(w)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(copyKeyJS))
}
func publicHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}
func (h *Handler) subscriptionPage(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/subscriptionPage"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	data, err := h.publicPage(r)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	var output bytes.Buffer
	if err := subscriptionTemplate.Execute(&output, data); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	_, _ = w.Write(output.Bytes())
}
func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/manifest"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	data, err := h.publicPage(r)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	branding, err := h.store.SubscriptionBranding(r.Context(), requestID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	data.Branding = &branding
	httpx.Respond(w, 200, data)
}
func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/profile"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	deviceID := chi.URLParam(r, "deviceID")
	if deviceID != "" && !httpx.ValidID(deviceID) {
		httpx.Failure(w, r, 400, "invalid device ID")
		return
	}
	profile, err := h.store.Profile(r.Context(), requestID, chi.URLParam(r, "token"), id, deviceID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": profileFilename(profile.Name, "conf")}))
	_, _ = w.Write([]byte(profile.Conf()))
}
func (h *Handler) amneziaProfile(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/amneziaProfile"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	h.writeAmneziaKey(w, r, true)
}
func (h *Handler) amneziaKey(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/amneziaKey"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	h.writeAmneziaKey(w, r, false)
}
func (h *Handler) writeAmneziaKey(w http.ResponseWriter, r *http.Request, attachment bool) {
	const op = "internal/api/publicsubscription/writeAmneziaKey"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	deviceID := chi.URLParam(r, "deviceID")
	if deviceID != "" && !httpx.ValidID(deviceID) {
		httpx.Failure(w, r, 400, "invalid device ID")
		return
	}
	profile, err := h.store.Profile(r.Context(), requestID, chi.URLParam(r, "token"), id, deviceID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	link, err := profile.AmneziaLink()
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	if !attachment {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(link))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": profileFilename(profile.Name, "vpn")}))
	_, _ = w.Write([]byte(link))
}

func profileFilename(name, extension string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 || r == 127 {
			return '-'
		}
		return r
	}, name)
	name = strings.Trim(strings.TrimSpace(name), ". -")
	if name == "" {
		name = "AmneziaWG"
	}
	return name + "." + extension
}
func (h *Handler) configArchive(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/configArchive"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	format := chi.URLParam(r, "format")
	if format != "vpn" && format != "conf" {
		httpx.Failure(w, r, http.StatusNotFound, "unknown config format")
		return
	}
	profiles, err := h.store.PublicProfiles(r.Context(), requestID, chi.URLParam(r, "token"))
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	if len(profiles) == 0 {
		httpx.Failure(w, r, http.StatusNotFound, "no available profiles")
		return
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for i, profile := range profiles {
		content := profile.Conf()
		if format == "vpn" {
			content, err = profile.AmneziaLink()
			if err != nil {
				httpx.HandleError(w, r, err)
				return
			}
		}
		file, err := archive.Create(archiveFilename(profile.Name, i+1, format))
		if err == nil {
			_, err = file.Write([]byte(content))
		}
		if err != nil {
			httpx.HandleError(w, r, err)
			return
		}
	}
	if err := archive.Close(); err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="subscription-`+format+`.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(data.Len()))
	_, _ = data.WriteTo(w)
}
func archiveFilename(name string, index int, format string) string {
	prefix := strconv.Itoa(index) + "-"
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, name)
	safe = strings.Trim(safe, "-")
	if safe == "" {
		safe = "server"
	}
	safe = strings.TrimRight(safe[:min(len(safe), 15-len(prefix))], "-")
	return prefix + safe + "." + format
}
func (h *Handler) deviceLink(w http.ResponseWriter, r *http.Request) {
	const op = "internal/api/publicsubscription/deviceLink"
	requestID := middleware.GetReqID(r.Context())
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.DebugContext(r.Context(), "handling request")
	publicHeaders(w)
	id := chi.URLParam(r, "deviceID")
	if !httpx.ValidID(id) {
		httpx.Failure(w, r, 400, "invalid device ID")
		return
	}
	sub, _, err := h.store.PublicSubscription(r.Context(), requestID, chi.URLParam(r, "token"))
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	token, err := h.store.DeviceSubscriptionToken(r.Context(), requestID, sub.UserID, id, sub.ID)
	if err != nil {
		httpx.HandleError(w, r, err)
		return
	}
	httpx.Respond(w, 200, map[string]string{"url": h.publicURL + "/s/" + token})
}
