package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNoDirectoryListingServesFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/hello.txt", []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h := http.FileServer(noDirectoryListing(http.Dir(dir)))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if strings.TrimSpace(rr.Body.String()) != "hello" {
		t.Fatalf("body = %q, want hello", rr.Body.String())
	}
}

func TestNoDirectoryListingHidesDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Mkdir(dir+"/nested", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	h := http.FileServer(noDirectoryListing(http.Dir(dir)))
	for _, path := range []string{"/", "/nested/"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)

		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", path, rr.Code, http.StatusNotFound)
		}
	}
}
