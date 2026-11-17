package with

import "log/slog"

func WithOp(log *slog.Logger, op string) *slog.Logger {
	return log.With(slog.String("op", op))
}
func WithOpAndRequestID(log *slog.Logger, op string, requestID string) *slog.Logger {
	return log.With(slog.String("op", op), slog.String("request_id", requestID))
}
