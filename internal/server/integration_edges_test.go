package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEdgeCreate_AndDisconnect(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")
	to := createNode(t, c, "reasoning", "Reason B", "Because B is so")

	resp := formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {to.String()},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create edge: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := readBody(t, get(t, c, "/nodes/"+from.String()))
	if !strings.Contains(body, "Reason B") {
		t.Fatalf("legend missing target node title; excerpt: %s", snippet(body))
	}

	edge := singleEdge(t, from, to)

	// Disconnect from the destination side (HTMX-free POST, but still
	// allowed by the wiki-open delete rule).
	resp = formPost(t, c, "/nodes/"+to.String()+"/edges/"+edge.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disconnect: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body = readBody(t, get(t, c, "/nodes/"+from.String()))
	if strings.Contains(body, "Reason B") {
		t.Fatalf("disconnected edge still rendered on legend; excerpt: %s", snippet(body))
	}
}

func TestEdgeCreate_DuplicateRejected(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")
	to := createNode(t, c, "reasoning", "Reason B", "")

	resp := formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {to.String()},
	})
	resp.Body.Close()

	// Same (from, to, kind) — the unique constraint should produce a flash.
	resp = formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {to.String()},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "already exists") {
		t.Fatalf("expected duplicate-edge flash; got: %s", snippet(body))
	}
}

func TestEdgeCreate_RejectsSelfEdge(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "View A", "")

	resp := formPost(t, c, "/nodes/"+id.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {id.String()},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "cannot connect to itself") {
		t.Fatalf("expected self-edge flash; got: %s", snippet(body))
	}
}

func singleEdge(t *testing.T, from, to uuid.UUID) uuid.UUID {
	t.Helper()
	rows, err := testServer.queries.ListEdgesFromNode(context.Background(), from)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	for _, r := range rows {
		if r.ToID == to {
			return r.ID
		}
	}
	t.Fatalf("expected one edge from %s to %s, found none", from, to)
	return uuid.Nil
}
