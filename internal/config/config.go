package config

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
	LogLevel    slog.Level

	// PublicBaseURL is the absolute origin the browser sees (e.g.
	// "https://mindful.example.org"). Required only when at least one OAuth
	// provider is configured, because callback URLs are derived from it.
	PublicBaseURL string

	// SignupEnabled gates new-account creation via the email+password
	// signup form. When false, the form is replaced by a "signups closed"
	// page that still offers OAuth/SSO. OAuth callbacks continue to create
	// users — SSO is the intended fallback for closed instances.
	SignupEnabled bool

	// AdminUsers is the list of usernames that get the 'admin' role
	// granted on startup, reconciled idempotently. The intended use is
	// bootstrapping the first admin on a fresh install; subsequent role
	// changes happen through the admin UI. Unknown usernames are logged
	// and skipped.
	AdminUsers []string

	// DataDir is the writable base directory for persistent state owned
	// by the service (currently just uploads, but the natural home for
	// any future on-disk caches or generated assets). On NixOS this is
	// the systemd StateDirectory, /var/lib/mindful-social.
	DataDir string

	// UploadDir stores user-uploaded files served from /uploads/*.
	// Defaults to $DATA_DIR/uploads; override UPLOAD_DIR to point
	// elsewhere (e.g. a mounted object-storage bucket).
	UploadDir string
}

// Load reads configuration from environment variables.
// DATABASE_URL is required; everything else has sensible defaults.
// The logger receives warnings for misconfigured values that get
// silently replaced with defaults (e.g. an unrecognised boolean), so a
// typo in production at least leaves a trace in the log stream.
func Load(logger *slog.Logger) (Config, error) {
	if logger == nil {
		logger = slog.Default()
	}
	dataDir := envOr("DATA_DIR", ".")
	cfg := Config{
		ListenAddr:    envOr("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		LogLevel:      envLogLevel(logger, "LOG_LEVEL", slog.LevelInfo),
		PublicBaseURL: envOr("PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
		SignupEnabled: envBool(logger, "SIGNUP_ENABLED", true),
		AdminUsers:    envList("ADMIN_USERS"),
		DataDir:       dataDir,
		UploadDir:     envOr("UPLOAD_DIR", filepath.Join(dataDir, "uploads")),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if os.Getenv("PUBLIC_BASE_URL") == "" {
		logger.Warn("config: PUBLIC_BASE_URL unset, using default", "default", cfg.PublicBaseURL)
	}
	return cfg, nil
}

// LogLevelFromEnv returns the configured startup log level. Invalid values
// fall back silently here; Load logs the warning once the logger exists.
func LogLevelFromEnv() slog.Level {
	level, ok := parseLogLevel(os.Getenv("LOG_LEVEL"))
	if !ok {
		return slog.LevelInfo
	}
	return level
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envList splits a comma-separated env var, trims whitespace, and drops
// empty entries. Returns nil when the var is unset so callers can
// distinguish "no list configured" from "empty list".
func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func envLogLevel(logger *slog.Logger, key string, fallback slog.Level) slog.Level {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	level, ok := parseLogLevel(raw)
	if ok {
		return level
	}
	logger.Warn("config: unrecognised log level, using default", "key", key, "value", raw, "default", fallback.String())
	return fallback
}

func parseLogLevel(raw string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// envBool parses common true/false spellings; anything unrecognised falls
// back to the default and logs a warning so a typo doesn't silently flip
// a flag — the prior behaviour was to swallow it.
func envBool(logger *slog.Logger, key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	switch strings.ToLower(raw) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	logger.Warn("config: unrecognised boolean, using default", "key", key, "value", raw, "default", fallback)
	return fallback
}
