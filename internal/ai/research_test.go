package ai

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},          // loopback
		{"::1", true},                // loopback v6
		{"10.0.0.5", true},           // private
		{"192.168.1.1", true},        // private
		{"172.16.0.1", true},         // private
		{"169.254.169.254", true},    // link-local / cloud metadata
		{"fe80::1", true},            // link-local v6
		{"fd00::1", true},            // unique-local v6
		{"0.0.0.0", true},            // unspecified
		{"224.0.0.1", true},          // multicast
		{"8.8.8.8", false},           // public
		{"1.1.1.1", false},           // public
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

func TestFetcherBlocksLoopbackServer(t *testing.T) {
	// httptest binds to 127.0.0.1, so a correct SSRF guard must refuse it even
	// though the server is real and reachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html><body>secret internal page</body></html>")
	}))
	defer srv.Close()

	_, err := NewFetcher().Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected loopback fetch to be blocked, got nil error")
	}
	if !strings.Contains(err.Error(), "non-public") {
		t.Errorf("error should mention the block, got %v", err)
	}
}

func TestFetcherRejectsNonHTTPScheme(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "gopher://x"} {
		if _, err := NewFetcher().Fetch(context.Background(), u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
}

func TestExtractText(t *testing.T) {
	doc := `<html><head><title>  Rent Control  </title></head>
	<body>
		<nav>home about contact</nav>
		<p>Rent control caps how much landlords can raise rent.</p>
		<script>var tracking = 1;</script>
		<style>.ad { color: red; }</style>
		<footer>copyright 2026</footer>
	</body></html>`
	title, text, err := extractText(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if title != "Rent Control" {
		t.Errorf("title = %q, want %q", title, "Rent Control")
	}
	if !strings.Contains(text, "Rent control caps how much landlords") {
		t.Errorf("body text missing content: %q", text)
	}
	for _, bad := range []string{"var tracking", "color: red", "home about contact", "copyright"} {
		if strings.Contains(text, bad) {
			t.Errorf("extracted text should not contain %q: %q", bad, text)
		}
	}
}

func TestSearchClientParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("expected format=json, got %q", r.URL.RawQuery)
		}
		io.WriteString(w, `{"results":[{"url":"https://a.example/1","title":"A"},{"url":"https://b.example/2","title":"B"},{"url":"https://c.example/3","title":"C"}]}`)
	}))
	defer srv.Close()

	hits, err := NewSearchClient(srv.URL).Search(context.Background(), "rent control", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2 (limit)", len(hits))
	}
	if hits[0].URL != "https://a.example/1" || hits[0].Title != "A" {
		t.Errorf("unexpected first hit: %+v", hits[0])
	}
}

func TestGatherFailsOnBlockedUserURL(t *testing.T) {
	// A user-supplied URL that can't be fetched (here: loopback, blocked by the
	// SSRF guard) must fail the whole request, not be silently skipped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	g := NewGatherer("", nil) // no search backend
	_, err := g.Gather(context.Background(), "prompt", []string{srv.URL}, false)
	if err == nil {
		t.Fatal("expected Gather to fail on a blocked user URL")
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Errorf("error should name the unreadable URL, got %v", err)
	}
}
