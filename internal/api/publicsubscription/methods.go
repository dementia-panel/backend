package publicsubscription

import (
	"bytes"
	"html/template"
	"mime"
	"net/http"
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
}

type page struct {
	Name      string     `json:"name"`
	ExpiresAt time.Time  `json:"expires_at"`
	Locations []download `json:"locations"`
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
			d.URL = h.publicURL + "/s/" + token + "/nodes/" + l.ID + ".conf"
			d.AmneziaURL = h.publicURL + "/s/" + token + "/nodes/" + l.ID + ".vpn"
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
<main>
<h1>{{.Name}}</h1>
<p>Действует до {{.ExpiresAt.UTC.Format "02.01.2006 15:04"}} UTC.</p>
<p>Выберите сервер и формат для своего приложения.</p>
<ul>{{range .Locations}}<li>{{if .URL}}<a href="{{.URL}}">{{.Name}} — AmneziaWG (.conf)</a> · <a href="{{.AmneziaURL}}">AmneziaVPN (.vpn)</a>{{else}}{{.Name}} — профиль пока недоступен{{end}}</li>{{end}}</ul>
</main>
</html>`))

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
	_, _ = w.Write(output.Bytes())
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	publicHeaders(w)
	data, err := h.publicPage(r)
	if err != nil {
		httpx.HandleError(w, err)
		return
	}
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
