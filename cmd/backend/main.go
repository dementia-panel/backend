package main

import (
	"context"
	"dementia/backend/internal/api"
	"dementia/backend/internal/infrastructure/config"
	"dementia/backend/internal/infrastructure/logger"
	"dementia/backend/internal/infrastructure/postgresql"
	"dementia/backend/internal/infrastructure/redis"
	"dementia/backend/internal/integration"
	"dementia/backend/internal/lib/logger/sl"
	"dementia/backend/internal/ratelimit"
	"dementia/backend/internal/store"
	"dementia/backend/internal/vpn"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func run() error {
	slog.SetDefault(logger.NewLogger("prod"))
	c, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.NewLogger(c.Mode)
	if c.LogLevel != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
			return errors.New("invalid LOG_LEVEL")
		}
		slog.SetDefault(slog.New(logger.Filter(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))))
	} else {
		slog.SetDefault(log)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := postgresql.New(ctx, c)
	if err != nil {
		return err
	}
	defer database.Close()
	db := database.Pool
	vault, err := vpn.NewVault(c.VaultKey)
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dbStore := store.New(db, vault)
	if err := dbStore.RecoverInstallations(startup, middleware.GetReqID(startup)); err != nil {
		return errors.New("could not recover interrupted installations")
	}
	app := api.NewAPI(dbStore, c.SigningKey, c.PublicURL, ctx, db.Ping)
	if c.RedisURL != "" {
		cache, err := redis.New(ctx, c.RedisURL)
		if err != nil {
			return err
		}
		defer cache.Close()
		app.LoginLimiter = ratelimit.Login{Client: cache}
		app.Ready = func(ctx context.Context) error {
			if err := db.Ping(ctx); err != nil {
				return err
			}
			return cache.Ping(ctx).Err()
		}
	}
	webhookStatus := integration.NewTracker(c.WebhookURL != "")
	bot := initializeTelegram(app, c, dbStore, webhookStatus)
	address := c.HTTPAddr
	server := &http.Server{Addr: address, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	workers := startWorkers(ctx, stop, database, dbStore, c, bot, webhookStatus)
	defer func() {
		stop()
		workers.Wait()
	}()
	result := make(chan error, 1)
	slog.Info("backend listening", "address", address)
	go func() {
		result <- server.ListenAndServe()
	}()
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

func main() {
	if err := run(); err != nil {
		slog.Error("backend stopped", sl.Err(err))
		os.Exit(1)
	}
}
