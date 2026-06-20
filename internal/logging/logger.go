package logging

import (
	"log/slog"
	"os"
	"strings"
	"unicode/utf8"
)

func ConfigureDefault() *slog.Logger {
	logger := New()
	slog.SetDefault(logger)
	return logger
}

func New() *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn", "warning":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}

	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(os.Getenv("LOG_ADD_SOURCE"), "true") {
		opts.AddSource = true
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT"))) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	default:
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
}

func TextMetricAttrs(prefix string, text string) []any {
	return []any{
		prefix + "_chars", utf8.RuneCountInString(text),
		prefix + "_bytes", len(text),
	}
}
