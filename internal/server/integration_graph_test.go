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
