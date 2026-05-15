package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoadReadsLogLevel(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
}

func TestLoadWarnsAndDefaultsInvalidLogLevel(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	t.Setenv("LOG_LEVEL", "verbose")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg, err := Load(logger)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if !strings.Contains(buf.String(), "unrecognised log level") {
		t.Fatalf("expected invalid log level warning, got %q", buf.String())
	}
}
