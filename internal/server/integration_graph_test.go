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
