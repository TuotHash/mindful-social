package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

func TestArgumentGraph_RendersVisibleNodesAndEdges(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	from := createNode(t, c, "view", "Graph view A", "")
	to := createNode(t, c, "finding", "Graph finding B", "")

	resp := formPost(t, c, "/nodes/"+from.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {to.String()},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create edge: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := readBody(t, get(t, c, "/graph"))
	for _, want := range []string{"Argument graph", "Graph view A", "Graph finding B", "supports", "data-argument-graph"} {
		if !strings.Contains(body, want) {
			t.Fatalf("graph page missing %q; excerpt: %s", want, snippet(body))
		}
	}
}

func TestArgumentGraphData_SearchesServerSide(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	createNode(t, c, "view", "Nuclear graph search result", "Body about reactors.")
	createNode(t, c, "view", "Solar panels everywhere", "")

	resp := get(t, c, "/graph/data?"+url.Values{"q": {"nuclear"}}.Encode())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data status = %d", resp.StatusCode)
	}
	var data views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode graph data: %v", err)
	}

	var foundNuclear, foundSolar bool
	for _, node := range data.Nodes {
		if node.Title == "Nuclear graph search result" {
			foundNuclear = true
		}
		if node.Title == "Solar panels everywhere" {
			foundSolar = true
		}
	}
	if !foundNuclear {
		t.Fatalf("/graph/data should include matching node, data: %+v", data.Nodes)
	}
	if foundSolar {
		t.Fatalf("/graph/data should not include unrelated node, data: %+v", data.Nodes)
	}
}

// TestArgumentGraphData_SearchIncludesNeighborhood pins the contract the
// depth slider depends on: when a search matches a single node, the
// response still carries that node's connected neighbours (and the edges
// between them). The unrelated node sits beyond the configured hop cap
// (a deliberately large gap is hard to construct in a unit test, so we
// verify the simpler "1 hop is included" case here).
func TestArgumentGraphData_SearchIncludesNeighborhood(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	match := createNode(t, c, "view", "Nuclear neighbourhood seed", "")
	neighbour := createNode(t, c, "finding", "Reactor backstop study", "")
	unrelated := createNode(t, c, "view", "Solar panels everywhere", "")

	resp := formPost(t, c, "/nodes/"+match.String()+"/edges", url.Values{
		"kind":  {"supports"},
		"to_id": {neighbour.String()},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create edge: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = get(t, c, "/graph/data?"+url.Values{"q": {"nuclear"}}.Encode())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data status = %d", resp.StatusCode)
	}

	var data views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode graph data: %v", err)
	}

	byID := make(map[string]views.ArgumentGraphNode, len(data.Nodes))
	for _, node := range data.Nodes {
		byID[node.ID] = node
	}
	matchedNode, matchedOK := byID[match.String()]
	if !matchedOK {
		t.Fatalf("response missing search match; nodes: %+v", data.Nodes)
	}
	if !matchedNode.Match {
		t.Fatalf("direct match should have match=true; node: %+v", matchedNode)
	}
	neighbourNode, neighbourOK := byID[neighbour.String()]
	if !neighbourOK {
		t.Fatalf("response missing 1-hop neighbour; nodes: %+v", data.Nodes)
	}
	if neighbourNode.Match {
		t.Fatalf("neighbour should have match=false; node: %+v", neighbourNode)
	}
	if _, ok := byID[unrelated.String()]; ok {
		t.Fatalf("response leaked unrelated node; nodes: %+v", data.Nodes)
	}

	var foundEdge bool
	for _, edge := range data.Edges {
		if (edge.FromID == match.String() && edge.ToID == neighbour.String()) ||
			(edge.FromID == neighbour.String() && edge.ToID == match.String()) {
			foundEdge = true
			break
		}
	}
	if !foundEdge {
		t.Fatalf("response missing edge between match and neighbour; edges: %+v", data.Edges)
	}
}

// TestArgumentGraphData_AuthorFilter pins the contract of the author
// filter on /graph/data: when ?author=username is set, only nodes
// authored by that user (and their neighborhood) come back. A node by
// another author with no edge to the filtered author's nodes must not
// leak through.
func TestArgumentGraphData_AuthorFilter(t *testing.T) {
	integrationDB(t)

	alice := newClient(t)
	signup(t, alice, "alicegraph", "alicegraph@example.com", "correct horse battery staple")
	aliceNode := createNode(t, alice, "view", "Alice graph view "+uuid.NewString()[:8], "")

	bob := newClient(t)
	signup(t, bob, "bobgraph", "bobgraph@example.com", "correct horse battery staple")
	bobNode := createNode(t, bob, "view", "Bob graph view "+uuid.NewString()[:8], "")

	resp := get(t, alice, "/graph/data?"+url.Values{"author": {"alicegraph"}}.Encode())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data status = %d", resp.StatusCode)
	}
	var data views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode graph data: %v", err)
	}

	byID := make(map[string]views.ArgumentGraphNode, len(data.Nodes))
	for _, node := range data.Nodes {
		byID[node.ID] = node
	}
	aliceSeen, aliceOK := byID[aliceNode.String()]
	if !aliceOK {
		t.Fatalf("response missing alice's node; nodes: %+v", data.Nodes)
	}
	if !aliceSeen.Match {
		t.Fatalf("alice's node should be a seed (Match=true) under author filter; node: %+v", aliceSeen)
	}
	if _, ok := byID[bobNode.String()]; ok {
		t.Fatalf("response leaked bob's unrelated node under author=alicegraph; nodes: %+v", data.Nodes)
	}

	// Unknown username returns an empty result rather than an error.
	resp2 := get(t, alice, "/graph/data?"+url.Values{"author": {"nobody-here-" + uuid.NewString()[:6]}}.Encode())
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data unknown-author status = %d", resp2.StatusCode)
	}
	var emptyData views.ArgumentGraphData
	if err := json.NewDecoder(resp2.Body).Decode(&emptyData); err != nil {
		t.Fatalf("decode empty data: %v", err)
	}
	if len(emptyData.Nodes) != 0 {
		t.Fatalf("unknown author should yield zero nodes, got %d", len(emptyData.Nodes))
	}
}

// TestArgumentGraphData_AuthorAndQueryIntersect verifies that combining
// ?q= and ?author= behaves as an AND: only nodes by `author` whose
// title/body matches `q` are seeded. The control node (same author,
// non-matching title) and the unrelated node (matching title, different
// author) must both be excluded.
func TestArgumentGraphData_AuthorAndQueryIntersect(t *testing.T) {
	integrationDB(t)

	alice := newClient(t)
	signup(t, alice, "alicequery", "alicequery@example.com", "correct horse battery staple")
	aliceMatch := createNode(t, alice, "view", "Wind energy alice match", "")
	aliceMiss := createNode(t, alice, "view", "Solar arrays elsewhere", "")

	bob := newClient(t)
	signup(t, bob, "bobquery", "bobquery@example.com", "correct horse battery staple")
	bobNoise := createNode(t, bob, "view", "Wind energy bob noise", "")

	resp := get(t, alice, "/graph/data?"+url.Values{
		"q":      {"wind"},
		"author": {"alicequery"},
	}.Encode())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/graph/data status = %d", resp.StatusCode)
	}
	var data views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode graph data: %v", err)
	}

	byID := make(map[string]views.ArgumentGraphNode, len(data.Nodes))
	for _, node := range data.Nodes {
		byID[node.ID] = node
	}
	if _, ok := byID[aliceMatch.String()]; !ok {
		t.Fatalf("intersection should include alice's matching node; nodes: %+v", data.Nodes)
	}
	if _, ok := byID[aliceMiss.String()]; ok {
		t.Fatalf("intersection should exclude alice's non-matching node; nodes: %+v", data.Nodes)
	}
	if _, ok := byID[bobNoise.String()]; ok {
		t.Fatalf("intersection should exclude bob's matching-but-wrong-author node; nodes: %+v", data.Nodes)
	}
}

func TestArgumentGraph_RespectsVisibility(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	author := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")
	privateTitle := "Private graph topic " + uuid.NewString()

	_, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      privateTitle,
		Body:       "",
		CreatedBy:  author.ID,
		Slug:       "private-graph-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPrivate,
	})
	if err != nil {
		t.Fatalf("create private node: %v", err)
	}

	anon := newClient(t)
	anonBody := readBody(t, get(t, anon, "/graph"))
	if strings.Contains(anonBody, privateTitle) {
		t.Fatalf("anonymous graph leaked private node; excerpt: %s", snippet(anonBody))
	}

	authorBody := readBody(t, get(t, c, "/graph"))
	if !strings.Contains(authorBody, privateTitle) {
		t.Fatalf("author graph missing own private node; excerpt: %s", snippet(authorBody))
	}
}
