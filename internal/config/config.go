package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string

	// PublicBaseURL is the absolute origin the browser sees (e.g.
	// "https://mindful.example.org"). Required only when at least one OAuth
	// provider is configured, because callback URLs are derived from it.
	PublicBaseURL string

	// SignupEnabled gates new-account creation via the email+password
	// signup form. When false, the form is replaced by a "signups closed"
	// page that still offers OAuth/SSO. OAuth callbacks continue to create
	// users — SSO is the intended fallback for closed instances.
	SignupEnabled bool
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; everything else has sensible defaults.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:    envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		PublicBaseURL: envOr("PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
		SignupEnabled: envBool("SIGNUP_ENABLED", true),
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

// envBool parses common true/false spellings; anything unrecognised falls
// back to the default so a typo doesn't silently flip a flag.
func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}
