package main

import (
	"log/slog"
	"os"
)

func initLogger() *slog.Logger {
	sinker := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	})

	logger := slog.New(sinker)
	slog.SetDefault(logger)

	return logger
}
