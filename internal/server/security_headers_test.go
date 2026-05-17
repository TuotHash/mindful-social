package server

import (
	"strings"
	"testing"
)

func TestSecurityHeaders_SetOnHTMLResponses(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	resp := get(t, c, "/")
	defer resp.Body.Close()

	cases := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}
	for h, want := range cases {
		if got := resp.Header.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}

	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header missing")
	}
	for _, fragment := range []string{
		"default-src 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"base-uri 'self'",
		"object-src 'none'",
	} {
		if !strings.Contains(csp, fragment) {
			t.Errorf("CSP missing %q; full header: %s", fragment, csp)
		}
	}
}

func TestSecurityHeaders_SkippedOnUploads(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	resp := get(t, c, "/uploads/does-not-exist.jpg")
	defer resp.Body.Close()

	// The HTML CSP should not leak onto upload responses (404 here is fine
	// — uploadsHandler will set its own sandbox CSP in a later commit).
	if got := resp.Header.Get("Content-Security-Policy"); strings.Contains(got, "form-action") {
		t.Errorf("HTML CSP leaked onto /uploads/: %q", got)
	}
}
