package config

import (
	"encoding/base64"
	"net/url"
	"os"
	"strings"
	"testing"
)

func environment(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{"DATABASE_URL", "POSTGRES_HOST", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "PUBLIC_URL", "JWT_SIGNING_KEY", "ENCRYPTION_KEY", "NODE_PING_INTERVAL_SECONDS", "WEBHOOK_URL", "WEBHOOK_SECRET_HEADER", "TELEGRAM_BOT_TOKEN", "TELEGRAM_MODE", "TELEGRAM_CHAT_ID", "TELEGRAM_THREAD_ID", "TELEGRAM_WEBHOOK_SECRET", "REDIS_URL", "HTTP_ADDR", "MODE", "LOG_LEVEL"} {
		t.Setenv(key, "")
	}
	t.Setenv("MODE", "prod")
	t.Setenv("DATABASE_URL", "postgres://user:password@localhost/database?sslmode=disable")
	t.Setenv("PUBLIC_URL", "https://panel.example.com")
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("JWT_SIGNING_KEY", key)
	t.Setenv("ENCRYPTION_KEY", key)
}
func TestConfigurationValidation(t *testing.T) {
	for _, test := range []struct{ key, value, want string }{
		{"JWT_SIGNING_KEY", "invalid", "JWT_SIGNING_KEY"},
		{"ENCRYPTION_KEY", "invalid", "ENCRYPTION_KEY"},
		{"PUBLIC_URL", "http://panel.example.com", "PUBLIC_URL"},
		{"PUBLIC_URL", "https://panel.example.com/path", "PUBLIC_URL"},
		{"NODE_PING_INTERVAL_SECONDS", "0", "NODE_PING_INTERVAL_SECONDS"},
		{"MODE", "unknown", "MODE"},
		{"WEBHOOK_URL", "https://events.example.com", "WEBHOOK_SECRET_HEADER"},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			environment(t)
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s validation, got %v", test.want, err)
			}
		})
	}
}
func TestPostgresPasswordEscaping(t *testing.T) {
	environment(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_HOST", "db:5432")
	t.Setenv("POSTGRES_USER", "admin")
	t.Setenv("POSTGRES_PASSWORD", "p@ss:/?#word")
	t.Setenv("POSTGRES_DB", "dementia")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(config.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	if password != "p@ss:/?#word" {
		t.Fatal("database credentials corrupted")
	}
}
func TestDotEnvDoesNotOverrideProcessEnvironment(t *testing.T) {
	environment(t)
	if err := os.WriteFile(".env", []byte("PUBLIC_URL=https://another.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.PublicURL != "https://panel.example.com" {
		t.Fatal("dotenv overrode process environment")
	}
}
