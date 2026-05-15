package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/csrf"

	"github.com/TuotHash/mindful-social/internal/views"
)

// csrfMiddleware composes gorilla/csrf with a thin context bridge so
// templ templates can read the token via views.CSRFToken(ctx). The bridge
// runs inside csrf.Protect so csrf.Token(r) is populated by the time we
// copy it onto our own ctx key.
//
// Cookie security follows the public base URL: HTTPS turns on Secure;
// anything else leaves it off so local HTTP dev still works.
func csrfMiddleware(logger *slog.Logger, publicBaseURL string) (func(http.Handler) http.Handler, error) {
	key, err := loadCSRFKey()
	if err != nil {
		return nil, err
	}
	secure := secureCookieForPublicBaseURL(publicBaseURL)
	protect := csrf.Protect(key,
		csrf.Secure(secure),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.CookieName("mindful_csrf"),
		csrf.Path("/"),
		csrf.HttpOnly(true),
		csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.Warn("csrf rejected", "method", r.Method, "path", r.URL.Path, "reason", csrf.FailureReason(r))
			http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
		})),
	)
	bridge := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := views.WithCSRFToken(r.Context(), csrf.Token(r))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	// Over plain HTTP, csrf.Protect would still enforce HTTPS-style
	// Referer checks unless every request is flagged plaintext via ctx.
	// The flag is set on the request before csrf.Protect inspects it.
	plaintext := !secure
	return func(next http.Handler) http.Handler {
		protected := protect(bridge(next))
		if !plaintext {
			return protected
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			protected.ServeHTTP(w, csrf.PlaintextHTTPRequest(r))
		})
	}, nil
}

func secureCookieForPublicBaseURL(publicBaseURL string) bool {
	return strings.HasPrefix(publicBaseURL, "https://")
}

// loadCSRFKey honours CSRF_KEY (32 bytes, hex) if set so tokens survive
// restarts. Otherwise a fresh key is generated per process — the
// double-submit cookie is invalidated on restart and users reload to
// recover, which is acceptable for the current MVP.
func loadCSRFKey() ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv("CSRF_KEY")); raw != "" {
		b, err := hex.DecodeString(raw)
		if err != nil {
			return nil, errors.New("CSRF_KEY must be hex-encoded")
		}
		if len(b) != 32 {
			return nil, errors.New("CSRF_KEY must decode to 32 bytes")
		}
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
