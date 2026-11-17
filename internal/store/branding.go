package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type SubscriptionBranding struct {
	DefaultLanguage string `json:"default_language"`
	Name            string `json:"name"`
	Logo            string `json:"logo"`
}

func (s Store) SubscriptionBranding(ctx context.Context) (SubscriptionBranding, error) {
	var branding SubscriptionBranding
	err := s.DB.QueryRow(ctx, `SELECT name, logo, default_language FROM subscription_branding WHERE id`).Scan(&branding.Name, &branding.Logo, &branding.DefaultLanguage)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionBranding{Name: "dementia", DefaultLanguage: "en"}, nil
	}
	return branding, err
}

func (s Store) SaveSubscriptionBranding(ctx context.Context, name, logo, language string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO subscription_branding (id, name, logo, default_language) VALUES (true, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, logo=EXCLUDED.logo,default_language=EXCLUDED.default_language`, name, logo, language)
	return err
}
