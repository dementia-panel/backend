package config

import (
	"dementia/backend/internal/vpn"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL             string `env:"DATABASE_URL"`
	PostgresHost            string `env:"POSTGRES_HOST"`
	PostgresUser            string `env:"POSTGRES_USER"`
	PostgresPassword        string `env:"POSTGRES_PASSWORD"`
	PostgresDB              string `env:"POSTGRES_DB"`
	PublicURL               string `env:"PUBLIC_URL"`
	JWTSigningKey           string `env:"JWT_SIGNING_KEY"`
	EncryptionKey           string `env:"ENCRYPTION_KEY"`
	NodePingIntervalSeconds string `env:"NODE_PING_INTERVAL_SECONDS"`
	WebhookURL              string `env:"WEBHOOK_URL"`
	WebhookSecret           string `env:"WEBHOOK_SECRET_HEADER"`
	TelegramToken           string `env:"TELEGRAM_BOT_TOKEN"`
	TelegramMode            string `env:"TELEGRAM_MODE" envDefault:"polling"`
	TelegramChatID          string `env:"TELEGRAM_CHAT_ID"`
	TelegramThreadID        string `env:"TELEGRAM_THREAD_ID"`
	TelegramWebhookSecret   string `env:"TELEGRAM_WEBHOOK_SECRET"`
	RedisURL                string `env:"REDIS_URL"`
	HTTPAddr                string `env:"HTTP_ADDR" envDefault:"127.0.0.1:8080"`
	Mode                    string `env:"MODE" envDefault:"prod"`
	LogLevel                string `env:"LOG_LEVEL"`
	SigningKey              []byte
	VaultKey                []byte
	CheckInterval           time.Duration
	Bot                     Telegram
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("could not load .env")
	}
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, errors.New("invalid environment configuration")
	}
	switch c.Mode {
	case "local", "dev", "prod", "test":
	default:
		return nil, errors.New("MODE must be local, dev, prod or test")
	}
	databaseURL := c.DatabaseURL
	if host := c.PostgresHost; databaseURL == "" && host != "" {
		databaseURL = (&url.URL{Scheme: "postgres", Host: host, User: url.UserPassword(c.PostgresUser, c.PostgresPassword), Path: "/" + c.PostgresDB, RawQuery: "sslmode=disable"}).String()
	}
	dbURL, err := url.Parse(databaseURL)
	if err != nil || (dbURL.Scheme != "postgres" && dbURL.Scheme != "postgresql") {
		return nil, errors.New("DATABASE_URL must be a PostgreSQL URL")
	}
	publicURL, err := url.Parse(c.PublicURL)
	if err != nil || publicURL.Scheme != "https" || publicURL.Hostname() == "" || publicURL.User != nil || strings.Trim(publicURL.Path, "/") != "" || publicURL.RawPath != "" || publicURL.ForceQuery || publicURL.RawQuery != "" || publicURL.Fragment != "" {
		return nil, errors.New("PUBLIC_URL must be an HTTPS origin")
	}
	publicURL.Path = ""
	signingKey, err := base64.StdEncoding.DecodeString(c.JWTSigningKey)
	if err != nil || len(signingKey) != 32 {
		return nil, errors.New("JWT_SIGNING_KEY must be 32 bytes in base64")
	}
	encryptionKey, err := base64.StdEncoding.DecodeString(c.EncryptionKey)
	if err != nil {
		return nil, errors.New("ENCRYPTION_KEY must be base64")
	}
	_, err = vpn.NewVault(encryptionKey)
	if err != nil {
		return nil, err
	}
	checkInterval := 30 * time.Second
	if raw := c.NodePingIntervalSeconds; raw != "" {
		seconds, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || seconds == 0 {
			return nil, errors.New("NODE_PING_INTERVAL_SECONDS must be a positive integer")
		}
		checkInterval = time.Duration(seconds) * time.Second
	}
	webhookURL := strings.TrimSpace(c.WebhookURL)
	webhookSecret := c.WebhookSecret
	if webhookURL != "" {
		target, err := url.Parse(webhookURL)
		if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
			return nil, errors.New("WEBHOOK_URL must be an HTTP or HTTPS URL")
		}
		if webhookSecret == "" {
			return nil, errors.New("WEBHOOK_SECRET_HEADER is required with WEBHOOK_URL")
		}
	}
	botConfig, err := ParseTelegram(strings.TrimSpace(c.TelegramToken), strings.TrimSpace(c.TelegramMode), strings.TrimSpace(c.TelegramChatID), strings.TrimSpace(c.TelegramThreadID), strings.TrimSpace(c.TelegramWebhookSecret))
	if err != nil {
		return nil, err
	}
	c.DatabaseURL = databaseURL
	c.PublicURL = publicURL.String()
	c.SigningKey = signingKey
	c.VaultKey = encryptionKey
	c.CheckInterval = checkInterval
	c.WebhookURL = webhookURL
	c.Bot = botConfig
	return &c, nil
}
