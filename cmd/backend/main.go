package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"dementia/backend/internal/api"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/logging"
	"dementia/backend/internal/ratelimit"
	"dementia/backend/internal/store"
	"dementia/backend/internal/telegram"
	"dementia/backend/internal/vpn"
	"dementia/backend/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := logging.Configure(os.Getenv("LOG_LEVEL")); err != nil {
		slog.Error("invalid logging configuration", "error", err)
		os.Exit(1)
	}
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if host := os.Getenv("POSTGRES_HOST"); databaseURL == "" && host != "" {
		databaseURL = (&url.URL{
			Scheme: "postgres", Host: host,
			User: url.UserPassword(os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD")),
			Path: "/" + os.Getenv("POSTGRES_DB"), RawQuery: "sslmode=disable",
		}).String()
	}
	dbURL, err := url.Parse(databaseURL)
	if err != nil || (dbURL.Scheme != "postgres" && dbURL.Scheme != "postgresql") {
		return errors.New("DATABASE_URL must be a PostgreSQL URL")
	}
	publicURL, err := url.Parse(os.Getenv("PUBLIC_URL"))
	if err != nil || publicURL.Scheme != "https" || publicURL.Hostname() == "" || publicURL.User != nil ||
		strings.Trim(publicURL.Path, "/") != "" || publicURL.RawPath != "" || publicURL.ForceQuery || publicURL.RawQuery != "" || publicURL.Fragment != "" {
		return errors.New("PUBLIC_URL must be an HTTPS origin")
	}
	publicURL.Path = ""
	signingKey, err := base64.StdEncoding.DecodeString(os.Getenv("JWT_SIGNING_KEY"))
	if err != nil || len(signingKey) != 32 {
		return errors.New("JWT_SIGNING_KEY must be 32 bytes in base64")
	}

	encryptionKey, err := base64.StdEncoding.DecodeString(os.Getenv("ENCRYPTION_KEY"))
	if err != nil {
		return errors.New("ENCRYPTION_KEY must be base64")
	}
	vault, err := vpn.NewVault(encryptionKey)
	if err != nil {
		return err
	}

	checkInterval := 30 * time.Second
	if raw := os.Getenv("NODE_PING_INTERVAL_SECONDS"); raw != "" {
		seconds, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || seconds == 0 {
			return errors.New("NODE_PING_INTERVAL_SECONDS must be a positive integer")
		}
		checkInterval = time.Duration(seconds) * time.Second
	}
	webhookURL := strings.TrimSpace(os.Getenv("WEBHOOK_URL"))
	webhookSecret := os.Getenv("WEBHOOK_SECRET_HEADER")
	if webhookURL != "" {
		target, err := url.Parse(webhookURL)
		if err != nil || (target.Scheme != "https" && target.Scheme != "http") ||
			target.Hostname() == "" || target.User != nil || target.Fragment != "" {
			return errors.New("WEBHOOK_URL must be an HTTP or HTTPS URL")
		}
		if webhookSecret == "" {
			return errors.New("WEBHOOK_SECRET_HEADER is required with WEBHOOK_URL")
		}
	}

	botConfig, err := telegram.ParseConfig(
		strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")), strings.TrimSpace(os.Getenv("TELEGRAM_MODE")),
		strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")), strings.TrimSpace(os.Getenv("TELEGRAM_THREAD_ID")),
		strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("could not parse PostgreSQL configuration")
	}
	if webhookURL != "" {
		dbConfig.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "on"
	} else {
		dbConfig.ConnConfig.RuntimeParams["dementia.webhook_enabled"] = "off"
	}
	if botConfig.Token != "" {
		dbConfig.ConnConfig.RuntimeParams["dementia.telegram_enabled"] = "on"
	}
	dbConfig.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	dbConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return errors.New("could not initialize PostgreSQL pool")
	}
	defer db.Close()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// One backend owns the in-memory SSH jobs. A database lock prevents a second
	// instance from marking active jobs interrupted during its startup.
	lock, err := pgx.Connect(startup, databaseURL)
	if err != nil {
		return errors.New("could not connect to PostgreSQL")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = lock.Close(closeCtx)
	}()
	var locked bool
	if err := lock.QueryRow(startup, "SELECT pg_try_advisory_lock(4134971021)").Scan(&locked); err != nil || !locked {
		return errors.New("another panel instance holds the PostgreSQL process lock")
	}
	dbURL.Scheme = "pgx5"
	query := dbURL.Query()
	query.Set("connect_timeout", "5")
	query.Set("statement_timeout", "60000")
	query.Set("lock_timeout", "5000")
	dbURL.RawQuery = query.Encode()
	if err := store.Migrate(dbURL.String()); err != nil {
		return fmt.Errorf("could not apply PostgreSQL migrations (%T)", err)
	}
	dbStore := store.Store{DB: db, Vault: vault}
	if err := dbStore.RecoverInstallations(startup); err != nil {
		return errors.New("could not recover interrupted installations")
	}
	app := &api.API{Store: dbStore, JWTSigningKey: signingKey, PublicURL: publicURL.String(), Context: ctx, Ready: db.Ping}
	if raw := os.Getenv("REDIS_URL"); raw != "" {
		options, err := redis.ParseURL(raw)
		if err != nil {
			return errors.New("invalid REDIS_URL")
		}
		options.ContextTimeoutEnabled = true
		options.MaxRetries = -1
		cache := redis.NewClient(options)
		defer cache.Close()
		cacheCtx, cancelCache := context.WithTimeout(ctx, 3*time.Second)
		err = cache.Ping(cacheCtx).Err()
		cancelCache()
		if err != nil {
			return errors.New("could not connect to Redis")
		}
		app.LoginLimiter = ratelimit.Login{Client: cache}
		app.Ready = func(ctx context.Context) error {
			if err := db.Ping(ctx); err != nil {
				return err
			}
			return cache.Ping(ctx).Err()
		}
	}
	webhookStatus := integration.NewTracker(webhookURL != "")
	var bot *telegram.Bot
	if botConfig.Token != "" {
		bot = telegram.New(botConfig, dbStore)
		if bot.WebhookEnabled() {
			app.TelegramWebhook = bot.Webhook
		}
	}

	app.IntegrationStatus = func() integration.Snapshot {
		telegramStatus := integration.NewTracker(false).Snapshot()
		if bot != nil {
			telegramStatus = bot.Status()
		}
		return integration.Snapshot{Telegram: telegramStatus, Webhook: webhookStatus.Snapshot()}
	}
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{
		Addr: address, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probe, cancel := context.WithTimeout(ctx, 3*time.Second)
				err := lock.Ping(probe)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Error("PostgreSQL ownership connection lost")
					stop()
					return
				}
			}
		}
	})
	workers.Go(func() { worker.MonitorNodes(ctx, dbStore, checkInterval) })
	workers.Go(func() { worker.ResetTraffic(ctx, dbStore) })
	if bot != nil {
		workers.Go(func() { bot.Run(ctx, publicURL.String()) })
		workers.Go(func() { bot.DeliverNotifications(ctx) })
	}
	if webhookURL != "" {
		workers.Go(func() { worker.DeliverWebhooks(ctx, dbStore, webhookURL, webhookSecret, webhookStatus) })
	}
	defer func() {
		stop()
		workers.Wait()
	}()

	result := make(chan error, 1)
	slog.Info("backend listening", "address", address)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		stop()
		_ = server.Close()
		app.Workers.CloseAndWait()
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		app.Workers.CloseAndWait()
		return err
	}
}
