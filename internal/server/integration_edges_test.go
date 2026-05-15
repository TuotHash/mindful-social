package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

func TestEdgeCreate_AndDisconnect(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")
	to := createNode(t, c, "finding", "Reason B", "Because B is so")

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

	// Disconnect from the destination side (HTMX-free POST), allowed because
	// the edge touches the page node the user can edit.
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

func TestEdgeMutationsRequireEdgeTouchingPageNode(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	user := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")
	page := createNode(t, c, "view", "View A", "")
	from := createNode(t, c, "view", "View B", "")
	to := createNode(t, c, "finding", "Reason C", "Because C is so")

	edge, err := testServer.queries.CreateEdge(t.Context(), db.CreateEdgeParams{
		FromNode:  from,
		ToNode:    to,
		Kind:      db.EdgeKindSupports,
		CreatedBy: user.ID,
	})
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{"delete", "/nodes/" + page.String() + "/edges/" + edge.ID.String() + "/delete"},
		{"highlight", "/nodes/" + page.String() + "/edges/" + edge.ID.String() + "/highlight"},
		{"unhighlight", "/nodes/" + page.String() + "/edges/" + edge.ID.String() + "/unhighlight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := formPost(t, c, tc.path, url.Values{})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s unrelated edge: status %d, want 404", tc.name, resp.StatusCode)
			}
		})
	}

	stillThere := singleEdge(t, from, to)
	if stillThere != edge.ID {
		t.Fatalf("unrelated delete removed or changed edge: got %s want %s", stillThere, edge.ID)
	}
}

func TestEdgeCreate_DuplicateRejected(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")
	to := createNode(t, c, "finding", "Reason B", "")

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

func TestEdgeCreate_InlineFindingCreatesFindingAndEdge(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")

	resp := formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":              {"supports"},
		"to_mode":           {"new"},
		"new_finding_title": {"The 2023 IPCC synthesis report"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("inline finding edge: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := readBody(t, get(t, c, "/nodes/"+from.String()))
	if !strings.Contains(body, "The 2023 IPCC synthesis report") {
		t.Fatalf("source page missing the new finding; excerpt: %s", snippet(body))
	}

	// The new finding exists, is a finding, and is parented to the source.
	// The source view also carries an auto-created related edge to its
	// parent topic (set up by createNode for view-type seeds), so we look
	// for the supports edge specifically.
	rows, err := testServer.queries.ListEdgesFromNodeForViewer(t.Context(), db.ListEdgesFromNodeForViewerParams{
		FromNode: from,
		ViewerID: nil,
	})
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	var supportsTarget *uuid.UUID
	for _, row := range rows {
		if row.Kind == db.EdgeKindSupports {
			id := row.ToID
			supportsTarget = &id
			break
		}
	}
	if supportsTarget == nil {
		t.Fatalf("expected one supports edge from source, found none in %d edges", len(rows))
	}
	finding, err := testServer.queries.GetNode(t.Context(), *supportsTarget)
	if err != nil {
		t.Fatalf("get created finding: %v", err)
	}
	if finding.Type != db.NodeTypeFinding {
		t.Fatalf("created node type = %q, want finding", finding.Type)
	}
	if finding.ParentNodeID == nil || *finding.ParentNodeID != from {
		t.Fatalf("created finding parent_node_id = %v, want %v", finding.ParentNodeID, from)
	}
}

func TestEdgeCreate_InlineFindingRequiresTitle(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "View A", "")

	resp := formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":              {"supports"},
		"to_mode":           {"new"},
		"new_finding_title": {""},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "Type a title for the new finding.") {
		t.Fatalf("expected missing-title flash; got: %s", snippet(body))
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

func TestEdgePicker_PrefixMatchesPartialWord(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	source := createNode(t, c, "topic", "Energy policy", "")
	// The thing we're searching for. The body is intentionally short so a
	// false positive from the body wouldn't accidentally pass the test.
	createNode(t, c, "view", "Nuclear power is good", "")
	// A noise node so the test asserts targeted matching, not "any node".
	createNode(t, c, "view", "Solar panels everywhere", "")

	resp := get(t, c, "/nodes/"+source.String()+"/edges/picker?find=nuc")
	body := readBody(t, resp)
	if !strings.Contains(body, "Nuclear power is good") {
		t.Fatalf("picker fragment should match 'nuc' against 'Nuclear power'; body: %s", snippet(body))
	}
	if strings.Contains(body, "Solar panels everywhere") {
		t.Fatalf("picker fragment should not match noise node 'Solar panels'; body: %s", snippet(body))
	}
}

func TestEdgePicker_FuzzyMatchTypo(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	source := createNode(t, c, "topic", "Energy policy", "")
	createNode(t, c, "view", "Nuclear power is good", "")
	createNode(t, c, "view", "Solar panels everywhere", "")

	// Typo: "nucear" missing the 'l' from "nuclear". Trigram similarity
	// against "Nuclear power" is high enough to match.
	resp := get(t, c, "/nodes/"+source.String()+"/edges/picker?find=nucear")
	body := readBody(t, resp)
	if !strings.Contains(body, "Nuclear power is good") {
		t.Fatalf("picker should fuzzy-match 'nucear' to 'Nuclear power'; body: %s", snippet(body))
	}
	if strings.Contains(body, "Solar panels everywhere") {
		t.Fatalf("picker should not match unrelated 'Solar panels'; body: %s", snippet(body))
	}
}

func TestEdgePicker_InfixMatch(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	source := createNode(t, c, "topic", "Energy policy", "")
	createNode(t, c, "view", "Nuclear power is good", "")

	// Mid-word substring: "uclear" inside "Nuclear" — only trigrams handle
	// this; the previous tsquery prefix approach would have missed it.
	resp := get(t, c, "/nodes/"+source.String()+"/edges/picker?find=uclear")
	body := readBody(t, resp)
	if !strings.Contains(body, "Nuclear power is good") {
		t.Fatalf("picker should infix-match 'uclear' to 'Nuclear power'; body: %s", snippet(body))
	}
}

func singleEdge(t *testing.T, from, to uuid.UUID) uuid.UUID {
	t.Helper()
	rows, err := testServer.queries.ListEdgesFromNodeForViewer(context.Background(), db.ListEdgesFromNodeForViewerParams{FromNode: from})
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
