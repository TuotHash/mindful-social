package server

import (
	"net/http"
	"strings"
)

// securityHeaders sets framing, sniffing, and referrer headers on every HTML
// response, plus a Content-Security-Policy that locks the script and form
// surface down. Upload responses are handled separately by uploadsHandler
// because they need a stricter sandbox CSP and their own MIME handling.
//
// The CSP keeps 'unsafe-inline' for scripts on purpose: several templates
// still use inline onclick/onsubmit handlers (modal close, confirm dialogs).
// Migrating those to event delegation lets us drop 'unsafe-inline' and lean
// on a strict nonce policy; tracked as future work. The current policy
// already blocks remote script injection (script-src 'self') and the most
// common cross-page abuses (frame-ancestors, form-action, base-uri).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Uploads, static assets, and machine-readable endpoints set their
		// own headers — skip the HTML-oriented CSP for them so e.g.
		// /healthz remains a plain-text 200.
		if !skipSecurityHeaders(r.URL.Path) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Content-Security-Policy", htmlCSP)
		}
		next.ServeHTTP(w, r)
	})
}

const htmlCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"media-src 'self' blob:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'; " +
	"base-uri 'self'; " +
	"object-src 'none'"

// skipSecurityHeaders returns true for URL prefixes whose responses already
// carry their own headers (uploads handler) or are not HTML (static assets,
// health). Routes are matched by prefix because chi paths under /uploads/
// and /static/ have arbitrary suffixes.
func skipSecurityHeaders(path string) bool {
	switch {
	case strings.HasPrefix(path, "/uploads/"):
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	case path == "/healthz":
		return true
	}
	return false
}
