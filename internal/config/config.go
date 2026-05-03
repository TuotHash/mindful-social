package config

import (
	"errors"
	"os"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string

	// PublicBaseURL is the absolute origin the browser sees (e.g.
	// "https://mindful.example.org"). Required only when at least one OAuth
	// provider is configured, because callback URLs are derived from it.
	PublicBaseURL string
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; everything else has sensible defaults.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:    envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		PublicBaseURL: envOr("PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
