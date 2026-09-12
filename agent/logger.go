package agent

import (
	"log/slog"
	"os"
)

type loggerOptions struct {
	filename string
	level    slog.Level
}

type WithLoggerOption func(*loggerOptions)

func WithLoggerFilename(filename string) WithLoggerOption {
	return func(opts *loggerOptions) {
		opts.filename = filename
	}
}

func WithLoggerLevel(level string) WithLoggerOption {
	return func(opts *loggerOptions) {
		_ = opts.level.UnmarshalText([]byte(level))
	}
}

func UseJsonLog(options ...WithLoggerOption) {
	opts := &loggerOptions{
		level: slog.LevelInfo,
	}
	for _, option := range options {
		option(opts)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     opts.level,
	}))

	slog.SetDefault(logger)
}
