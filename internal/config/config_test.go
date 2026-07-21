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
	t.Setenv("GEMINI_API_KEY", "secret-key")
	t.Setenv("AI_PROVIDERS", `[
		{"name":"local","endpoint":"http://127.0.0.1:11434/v1","model":"llama3.1:8b"},
		{"name":"gemini","endpoint":"https://gemini.example/v1","model":"gemini-3-flash-preview","api_key_env":"GEMINI_API_KEY"}
	]`)

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
	// api_key_env resolves from the environment; the local one has no key.
	if cfg.AIProviders[0].APIKey != "" {
		t.Errorf("local provider should have no key, got %q", cfg.AIProviders[0].APIKey)
	}
	if cfg.AIProviders[1].APIKey != "secret-key" {
		t.Errorf("gemini key = %q, want resolved from GEMINI_API_KEY", cfg.AIProviders[1].APIKey)
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

func TestLoadAIProvidersMalformedDisables(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres:///mindful_social")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	// Set legacy vars too, to prove malformed JSON does NOT silently fall back.
	t.Setenv("AI_ENDPOINT_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("AI_PROVIDERS", `{not json`)

	var buf bytes.Buffer
	cfg, err := Load(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AIProviders) != 0 {
		t.Fatalf("malformed AI_PROVIDERS should disable AI, got %+v", cfg.AIProviders)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected a JSON warning, got %q", buf.String())
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
