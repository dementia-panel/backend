package logger

import (
	"bytes"
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

func TestFilterDropsRoutineTraceButKeepsDiagnostics(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(Filter(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.Debug("repository operation")
	log.Debug("repository transaction")
	log.Debug("handling request")
	log.Debug("node state changed", "node_id", "node-1")
	if output.String() == "" || bytes.Contains(output.Bytes(), []byte("repository")) || bytes.Contains(output.Bytes(), []byte("handling request")) {
		t.Fatalf("unexpected filtered output: %s", output.String())
	}
}
