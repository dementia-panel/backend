package logger

import (
	"context"
	"log/slog"
	"testing"
)

func TestLoggerModes(t *testing.T) {
	for _, test := range []struct {
		mode        string
		debug, info bool
	}{{"local", true, true}, {"dev", true, true}, {"prod", false, true}, {"test", false, false}} {
		log := NewLogger(test.mode)
		if log == nil || log.Enabled(context.Background(), slog.LevelDebug) != test.debug || log.Enabled(context.Background(), slog.LevelInfo) != test.info {
			t.Errorf("unexpected log level for %s", test.mode)
		}
	}
}
