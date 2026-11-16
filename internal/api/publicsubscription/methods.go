package publicsubscription

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"html/template"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dementia/backend/internal/api/httpx"
	"dementia/backend/internal/store"
	"github.com/go-chi/chi/v5"
)

type download struct {
	store.Location
	URL        string `json:"url,omitempty"`
	AmneziaURL string `json:"amnezia_url,omitempty"`
	KeyURL     string `json:"key_url,omitempty"`
}

type page struct {
	Name      string                      `json:"name"`
	ExpiresAt *time.Time                  `json:"expires_at"`
	Locations []download                  `json:"locations"`
	Branding  *store.SubscriptionBranding `json:"branding,omitempty"`
}

func (h *Handler) publicPage(r *http.Request) (page, error) {
	token := chi.URLParam(r, "token")
	sub, locations, err := h.store.PublicSubscription(r.Context(), token)
	if err != nil {
		return page{}, err
	}
	result := page{Name: sub.Name, ExpiresAt: sub.ExpiresAt, Locations: make([]download, 0, len(locations))}
	for _, l := range locations {
		d := download{Location: l}
		if l.Available {
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
	publicHeaders(w)
	data, err := h.publicPage(r)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	var output bytes.Buffer
	if err := subscriptionTemplate.Execute(&output, data); err != nil {
		httpx.HandleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	_, _ = w.Write(output.Bytes())
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	data, err := h.publicPage(r)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	branding, err := h.store.SubscriptionBranding(r.Context())
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	data.Branding = &branding
	httpx.Respond(w, 200, data)
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	profile, err := h.store.Profile(r.Context(), chi.URLParam(r, "token"), id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="dementia-`+id+`.conf"`)
	_, _ = w.Write([]byte(profile.Conf()))
}

func (h *Handler) amneziaProfile(w http.ResponseWriter, r *http.Request) {
	h.writeAmneziaKey(w, r, true)
}

func (h *Handler) amneziaKey(w http.ResponseWriter, r *http.Request) {
	h.writeAmneziaKey(w, r, false)
}

func (h *Handler) writeAmneziaKey(w http.ResponseWriter, r *http.Request, attachment bool) {
	publicHeaders(w)
	id := httpx.IDParam(w, r)
	if id == "" {
		return
	}
	profile, err := h.store.Profile(r.Context(), chi.URLParam(r, "token"), id)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	link, err := profile.AmneziaLink()
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	if !attachment {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(link))
		return
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 || r == 127 {
			return '-'
		}
		return r
	}, profile.Name)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".vpn"}))
	_, _ = w.Write([]byte(link))
}

func (h *Handler) configArchive(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	format := chi.URLParam(r, "format")
	if format != "vpn" && format != "conf" {
		httpx.Failure(w, http.StatusNotFound, "unknown config format")
		return
	}
	profiles, err := h.store.PublicProfiles(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
	if len(profiles) == 0 {
		httpx.Failure(w, http.StatusNotFound, "no available profiles")
		return
	}

	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for i, profile := range profiles {
		content := profile.Conf()
		if format == "vpn" {
			content, err = profile.AmneziaLink()
			if err != nil {
				httpx.HandleError(w, err)
				return
			}
		}
		file, err := archive.Create(archiveFilename(profile.Name, i+1, format))
		if err == nil {
			_, err = file.Write([]byte(content))
		}
		if err != nil {
			httpx.HandleError(w, err)
			return
		}
	}
	if err := archive.Close(); err != nil {
		httpx.HandleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="subscription-`+format+`.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(data.Len()))
	_, _ = data.WriteTo(w)
}

func archiveFilename(name string, index int, format string) string {
	// Keep tunnel names within the 15-character limit on AmneziaWG Android.
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
