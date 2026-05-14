package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/TuotHash/mindful-social/internal/config"
)

// integration tests share one *Server bound to TEST_DATABASE_URL. Each test
// truncates the data tables in t.Cleanup so cases stay isolated. If
// TEST_DATABASE_URL is empty the integration suite skips itself, leaving
// pure-function unit tests to run as usual.

var (
	testInitOnce sync.Once
	testInitErr  error
	testServer   *Server
	testTS       *httptest.Server
)

func integrationDB(t *testing.T) *Server {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test (run scripts/db-test-setup.sh)")
	}
	testInitOnce.Do(func() {
		uploadDir, err := os.MkdirTemp("", "mindful-social-test-uploads-*")
		if err != nil {
			testInitErr = err
			return
		}
		cfg := config.Config{
			ListenAddr:    "127.0.0.1:0",
			DatabaseURL:   url,
			PublicBaseURL: "http://127.0.0.1",
			SignupEnabled: true,
			UploadDir:     uploadDir,
		}
		// Discard logs in tests; failures surface via assertions.
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		s, err := New(cfg, logger)
		if err != nil {
			testInitErr = err
			return
		}
		testServer = s
		testTS = httptest.NewServer(s.Handler())
	})
	if testInitErr != nil {
		t.Fatalf("integration server setup failed: %v", testInitErr)
	}
	t.Cleanup(func() { truncateAll(t) })
	return testServer
}

// truncateAll wipes every data table the app touches, leaving the schema
// intact. Called from t.Cleanup so each test sees an empty database.
func truncateAll(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	// CASCADE picks up the FK chains. sessions has no FKs but is included
	// so logged-in state from a previous test doesn't leak.
	const sql = `TRUNCATE TABLE
		user_node_pins, node_tags, tags, edges, node_images, node_videos, nodes,
		group_invites, group_memberships, groups,
		auth_identities, sessions, users
		RESTART IDENTITY CASCADE`
	if _, err := testServer.db.Exec(ctx, sql); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
