package views

import (
	"strings"
	"testing"
)

func TestStaticAssetAppendsContentHash(t *testing.T) {
	got := string(StaticAsset("/static/app.js"))
	if !strings.HasPrefix(got, "/static/app.js?v=") {
		t.Fatalf("StaticAsset app.js = %q, want versioned URL", got)
	}
	if len(strings.TrimPrefix(got, "/static/app.js?v=")) != 12 {
		t.Fatalf("StaticAsset app.js = %q, want 12-character hash", got)
	}
}

func TestStaticAssetFallsBackForMissingAsset(t *testing.T) {
	const path = "/static/not-found.js"
	if got := string(StaticAsset(path)); got != path {
		t.Fatalf("StaticAsset missing asset = %q, want %q", got, path)
	}
}
