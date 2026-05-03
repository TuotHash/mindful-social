package config

import (
	"errors"
	"os"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; everything else has sensible defaults.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:  envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
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
