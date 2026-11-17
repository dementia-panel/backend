package publicsubscription

import (
	"dementia/backend/internal/store"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	store     store.Store
	publicURL string
}

func Register(r chi.Router, repo store.Store, publicURL string) {
	h := &Handler{store: repo, publicURL: publicURL}
	r.Get("/s/{token}", h.subscriptionPage)
	r.Get("/s/assets/copy-key.js", copyKeyScript)
	r.Get("/s/{token}/devices/{deviceID}/link", h.deviceLink)
	r.Get("/s/{token}/manifest", h.manifest)
	r.Get("/s/{token}/configs/{format}.zip", h.configArchive)
	r.Get("/s/{token}/nodes/{id}.conf", h.profile)
	r.Get("/s/{token}/nodes/{id}.vpn", h.amneziaProfile)
	r.Get("/s/{token}/nodes/{id}.key", h.amneziaKey)
	r.Get("/s/{token}/devices/{deviceID}/nodes/{id}.conf", h.profile)
	r.Get("/s/{token}/devices/{deviceID}/nodes/{id}.vpn", h.amneziaProfile)
	r.Get("/s/{token}/devices/{deviceID}/nodes/{id}.key", h.amneziaKey)
}
