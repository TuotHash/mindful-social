package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/db"
)

func TestPin_UpsertSwapsKindInPlace(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	user := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")
	view := createNode(t, c, "view", "Open source is good", "")

	// Pin as supports.
	resp := formPost(t, c, "/nodes/"+view.String()+"/pin", url.Values{
		"kind": {"supports"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin supports: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	row, err := testServer.queries.GetPinForUserAndNode(context.Background(), db.GetPinForUserAndNodeParams{
		UserID: user.ID,
		NodeID: view,
	})
	if err != nil {
		t.Fatalf("get pin after supports: %v", err)
	}
	if row.Kind != db.PinKindSupports {
		t.Fatalf("expected supports, got %s", row.Kind)
	}

	// Switch to opposes — same row, kind flipped, no second insert.
	resp = formPost(t, c, "/nodes/"+view.String()+"/pin", url.Values{
		"kind": {"opposes"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin opposes: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	row, err = testServer.queries.GetPinForUserAndNode(context.Background(), db.GetPinForUserAndNodeParams{
		UserID: user.ID,
		NodeID: view,
	})
	if err != nil {
		t.Fatalf("get pin after opposes: %v", err)
	}
	if row.Kind != db.PinKindOpposes {
		t.Fatalf("expected opposes, got %s", row.Kind)
	}

	// Detail page banner reflects the current kind.
	body := readBody(t, get(t, c, "/nodes/"+view.String()))
	if !strings.Contains(body, "You oppose this") {
		t.Fatalf("detail page should say 'You oppose this'; excerpt: %s", snippet(body))
	}
}

func TestPin_RejectsSupportsOnNonView(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topic := createNode(t, c, "topic", "Open source", "")

	resp := formPost(t, c, "/nodes/"+topic.String()+"/pin", url.Values{
		"kind": {"supports"},
	})
	body := readBody(t, resp)
	// Pin form re-renders with a flash. The handler accepts the kind only
	// for view-typed nodes; non-views fall through to a validation error.
	if !strings.Contains(body, "view") && !strings.Contains(body, "View") {
		t.Fatalf("expected validation flash mentioning views; got: %s", snippet(body))
	}
}

func TestUnpin_RemovesRow(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	user := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")
	view := createNode(t, c, "view", "View A", "")

	resp := formPost(t, c, "/nodes/"+view.String()+"/pin", url.Values{
		"kind": {"supports"},
	})
	resp.Body.Close()

	resp = formPost(t, c, "/nodes/"+view.String()+"/unpin", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unpin: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	_, err := testServer.queries.GetPinForUserAndNode(context.Background(), db.GetPinForUserAndNodeParams{
		UserID: user.ID,
		NodeID: view,
	})
	// No row — pgx returns ErrNoRows.
	if err == nil {
		t.Fatalf("expected ErrNoRows after unpin, got pin row")
	}
}
