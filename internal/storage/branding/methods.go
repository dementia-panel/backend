package branding

import (
	"context"
	"dementia/backend/internal/lib/logger/with"
	. "dementia/backend/internal/storage/shared"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

func (s Repository) SubscriptionBranding(ctx context.Context, requestID string) (SubscriptionBranding, error) {
	const op = "internal/storage/branding/SubscriptionBranding"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	var branding SubscriptionBranding
	err := s.DB.QueryRow(ctx, `SELECT name, logo, default_language FROM subscription_branding WHERE id`).Scan(&branding.Name, &branding.Logo, &branding.DefaultLanguage)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubscriptionBranding{Name: "dementia", DefaultLanguage: "en"}, nil
	}
	return branding, err
}
func (s Repository) SaveSubscriptionBranding(ctx context.Context, requestID string, name, logo, language string) error {
	const op = "internal/storage/branding/SaveSubscriptionBranding"
	log := with.WithOpAndRequestID(slog.Default(), op, requestID)
	log.Debug("repository operation")
	_, err := s.DB.Exec(ctx, `INSERT INTO subscription_branding (id, name, logo, default_language) VALUES (true, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, logo=EXCLUDED.logo,default_language=EXCLUDED.default_language`, name, logo, language)
	return err
}
