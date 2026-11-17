package main

import (
	"dementia/backend/internal/application"
	"dementia/backend/internal/lib/logger/sl"
	"log/slog"
	"os"
)

func main() {
	if err := application.Run(); err != nil {
		slog.Error("backend stopped", sl.Err(err))
		os.Exit(1)
	}
}
