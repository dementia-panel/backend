package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type SubscriptionBranding struct {
	Name string `json:"name"`
	Logo string `json:"logo"`
}

func (s Store) SubscriptionBranding(ctx context.Context) (SubscriptionBranding, error) {
	var branding SubscriptionBranding
	err := s.DB.QueryRow(ctx, `SELECT name, logo FROM subscription_branding WHERE id`).Scan(&branding.Name, &branding.Logo)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionBranding{Name: "dementia"}, nil
	}
	return branding, err
}

func (s Store) SaveSubscriptionBranding(ctx context.Context, name, logo string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO subscription_branding (id, name, logo) VALUES (true, $1, $2)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, logo=EXCLUDED.logo`, name, logo)
	return err
}
