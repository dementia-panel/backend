package branding_test

import (
	"dementia/backend/internal/store"
	"dementia/backend/internal/testdb"
	"dementia/backend/internal/vpn"
	"testing"
)

func TestBrandingPersistsDefaultLanguage(t *testing.T) {
	pool := testdb.Pool(t, store.Migrate)
	vault, err := vpn.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := store.New(pool, vault)
	ctx := t.Context()
	const requestID = "repository-test"
	if err := repo.SaveSubscriptionBranding(ctx, requestID, "Custom VPN", "", "ru"); err != nil {
		t.Fatal(err)
	}
	result, err := repo.SubscriptionBranding(ctx, requestID)
	if err != nil || result.Name != "Custom VPN" || result.DefaultLanguage != "ru" {
		t.Fatalf("branding: %+v %v", result, err)
	}
}
