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

func TestLoadParsesAIProviders(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	// OIDC-style scheme: a CSV of keys plus per-key AI_<KEY>_* vars.
	t.Setenv("AI_PROVIDERS", "local, gemini")
	t.Setenv("AI_LOCAL_ENDPOINT", "http://127.0.0.1:11434/v1")
	t.Setenv("AI_LOCAL_MODEL", "llama3.1:8b")
	t.Setenv("AI_GEMINI_ENDPOINT", "https://gemini.example/v1")
	t.Setenv("AI_GEMINI_MODEL", "gemini-3-flash-preview")
	t.Setenv("AI_GEMINI_API_KEY", "secret-key")

	cfg, err := Load(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AIProviders) != 2 {
		t.Fatalf("AIProviders len = %d, want 2: %+v", len(cfg.AIProviders), cfg.AIProviders)
	}
	// Order is preserved (failover tries first-to-last).
	if cfg.AIProviders[0].Name != "local" || cfg.AIProviders[1].Name != "gemini" {
		t.Fatalf("providers not in order: %+v", cfg.AIProviders)
	}
	if cfg.AIProviders[0].Endpoint != "http://127.0.0.1:11434/v1" || cfg.AIProviders[0].Model != "llama3.1:8b" {
		t.Errorf("local provider mapped wrong: %+v", cfg.AIProviders[0])
	}
	// AI_<KEY>_API_KEY is the secret; the local one has none.
	if cfg.AIProviders[0].APIKey != "" {
		t.Errorf("local provider should have no key, got %q", cfg.AIProviders[0].APIKey)
	}
	if cfg.AIProviders[1].APIKey != "secret-key" {
		t.Errorf("gemini key = %q, want AI_GEMINI_API_KEY", cfg.AIProviders[1].APIKey)
	}
}

func TestLoadAIProvidersLegacyFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	t.Setenv("AI_ENDPOINT_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("AI_MODEL", "llama3.1:8b")
	t.Setenv("AI_API_KEY", "legacy-key")

	cfg, err := Load(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AIProviders) != 1 {
		t.Fatalf("expected one legacy provider, got %+v", cfg.AIProviders)
	}
	p := cfg.AIProviders[0]
	if p.Name != "default" || p.Endpoint != "http://127.0.0.1:11434/v1" || p.Model != "llama3.1:8b" || p.APIKey != "legacy-key" {
		t.Errorf("legacy provider mapped wrong: %+v", p)
	}
}

func TestLoadAIProvidersIncompleteSkipped(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	// One complete provider and one missing its model; the incomplete one is
	// warned and skipped rather than failing the whole list.
	t.Setenv("AI_PROVIDERS", "local,broken")
	t.Setenv("AI_LOCAL_ENDPOINT", "http://127.0.0.1:11434/v1")
	t.Setenv("AI_LOCAL_MODEL", "llama3.1:8b")
	t.Setenv("AI_BROKEN_ENDPOINT", "http://broken.example/v1") // no AI_BROKEN_MODEL

	var buf bytes.Buffer
	cfg, err := Load(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AIProviders) != 1 || cfg.AIProviders[0].Name != "local" {
		t.Fatalf("expected only the complete 'local' provider, got %+v", cfg.AIProviders)
	}
	if !strings.Contains(buf.String(), "skipping incomplete AI provider") {
		t.Errorf("expected an incomplete-provider warning, got %q", buf.String())
	}
}

func TestLoadAIProvidersSetButAllIncompleteDisables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	// AI_PROVIDERS is set but no provider is fully defined; this must NOT
	// silently fall back to the legacy vars (that would mask the misconfig).
	t.Setenv("AI_ENDPOINT_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("AI_PROVIDERS", "ghost")

	cfg, err := Load(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AIProviders) != 0 {
		t.Fatalf("expected AI disabled, got %+v", cfg.AIProviders)
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
