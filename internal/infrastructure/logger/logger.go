package logger

import (
	"context"
	"log/slog"
	"os"
)

type filteredHandler struct{ next slog.Handler }

func (h filteredHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h filteredHandler) Handle(ctx context.Context, record slog.Record) error {
	switch record.Message {
	case "repository operation", "repository transaction", "handling request":
		return nil
	}
	return h.next.Handle(ctx, record)
}

func (h filteredHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return filteredHandler{next: h.next.WithAttrs(attrs)}
}

func (h filteredHandler) WithGroup(name string) slog.Handler {
	return filteredHandler{next: h.next.WithGroup(name)}
}

func Filter(handler slog.Handler) slog.Handler {
	return filteredHandler{next: handler}
}

func NewLogger(mode string) *slog.Logger {
	switch mode {
	case "local":
		return slog.New(Filter(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	case "dev":
		return slog.New(Filter(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	case "test":
		return slog.New(slog.DiscardHandler)
	default:
		return slog.New(Filter(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}
}
