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

// TestArgumentGraphData_SourcedFilter pins the contract of ?sourced=yes /
// ?sourced=no: only nodes with (or without) a source_url come back as
// seeds. Nodes returned via neighbourhood expansion don't need to match
// the predicate — only the seed itself does — so the test asserts the
// match flags on the seed nodes.
func TestArgumentGraphData_SourcedFilter(t *testing.T) {
	integrationDB(t)

	c := newClient(t)
	user := signupAndGetUser(t, c, "alicesource", "alicesource@example.com", "correct horse battery staple")

	src := "https://example.com/paper"
	sourced, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Sourced topic " + uuid.NewString()[:8],
		Body:       "",
		SourceUrl:  &src,
		CreatedBy:  user.ID,
		Slug:       "sourced-topic-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPublic,
	})
	if err != nil {
		t.Fatalf("create sourced node: %v", err)
	}
	unsourced, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Unsourced topic " + uuid.NewString()[:8],
		Body:       "",
		CreatedBy:  user.ID,
		Slug:       "unsourced-topic-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPublic,
	})
	if err != nil {
		t.Fatalf("create unsourced node: %v", err)
	}

	cases := []struct {
		name      string
		query     string
		wantSeed  uuid.UUID
		denySeed  uuid.UUID
	}{
		{"sourced=yes", "sourced=yes", sourced.ID, unsourced.ID},
		{"sourced=no", "sourced=no", unsourced.ID, sourced.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := get(t, c, "/graph/data?"+tc.query)
			defer resp.Body.Close()
			var data views.ArgumentGraphData
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				t.Fatalf("decode: %v", err)
			}
			byID := make(map[string]views.ArgumentGraphNode, len(data.Nodes))
			for _, n := range data.Nodes {
				byID[n.ID] = n
			}
			want, ok := byID[tc.wantSeed.String()]
			if !ok {
				t.Fatalf("expected seed %s missing; nodes=%+v", tc.wantSeed, data.Nodes)
			}
			if !want.Match {
				t.Fatalf("expected node to be a seed (Match=true); got %+v", want)
			}
			if got, leaked := byID[tc.denySeed.String()]; leaked && got.Match {
				t.Fatalf("filter leaked %s as a seed: %+v", tc.denySeed, got)
			}
		})
	}
}

// TestArgumentGraphData_VisibilityFilter exercises ?visibility=private —
// only the viewer's own private nodes should surface, never a public node.
func TestArgumentGraphData_VisibilityFilter(t *testing.T) {
	integrationDB(t)

	c := newClient(t)
	user := signupAndGetUser(t, c, "alicepriv", "alicepriv@example.com", "correct horse battery staple")

	private, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Private only " + uuid.NewString()[:8],
		Body:       "",
		CreatedBy:  user.ID,
		Slug:       "private-only-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPrivate,
	})
	if err != nil {
		t.Fatalf("create private node: %v", err)
	}
	public, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Public only " + uuid.NewString()[:8],
		Body:       "",
		CreatedBy:  user.ID,
		Slug:       "public-only-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPublic,
	})
	if err != nil {
		t.Fatalf("create public node: %v", err)
	}

	resp := get(t, c, "/graph/data?visibility=private")
	defer resp.Body.Close()
	var data views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := make(map[string]views.ArgumentGraphNode, len(data.Nodes))
	for _, n := range data.Nodes {
		byID[n.ID] = n
	}
	if seed, ok := byID[private.ID.String()]; !ok || !seed.Match {
		t.Fatalf("private node should be a seed under visibility=private; nodes=%+v", data.Nodes)
	}
	if seed, ok := byID[public.ID.String()]; ok && seed.Match {
		t.Fatalf("public node should not be a seed under visibility=private; got %+v", seed)
	}
}

// TestArgumentGraphData_TagFilter pins single-tag and multi-tag
// behaviour: passing two tags only seeds nodes carrying both.
func TestArgumentGraphData_TagFilter(t *testing.T) {
	integrationDB(t)

	c := newClient(t)
	user := signupAndGetUser(t, c, "alicetag", "alicetag@example.com", "correct horse battery staple")

	both, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Has both tags " + uuid.NewString()[:8],
		CreatedBy:  user.ID,
		Slug:       "both-tags-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPublic,
	})
	if err != nil {
		t.Fatalf("create both-tags node: %v", err)
	}
	onlyA, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      "Has only tag a " + uuid.NewString()[:8],
		CreatedBy:  user.ID,
		Slug:       "only-a-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindPublic,
	})
	if err != nil {
		t.Fatalf("create only-a node: %v", err)
	}

	tagA := "graph-filter-a-" + uuid.NewString()[:6]
	tagB := "graph-filter-b-" + uuid.NewString()[:6]
	tagAID, err := testServer.queries.UpsertTag(t.Context(), tagA)
	if err != nil {
		t.Fatalf("upsert tag a: %v", err)
	}
	tagBID, err := testServer.queries.UpsertTag(t.Context(), tagB)
	if err != nil {
		t.Fatalf("upsert tag b: %v", err)
	}
	for _, attach := range []db.AttachTagParams{
		{NodeID: both.ID, TagID: tagAID},
		{NodeID: both.ID, TagID: tagBID},
		{NodeID: onlyA.ID, TagID: tagAID},
	} {
		if err := testServer.queries.AttachTag(t.Context(), attach); err != nil {
			t.Fatalf("attach tag: %v", err)
		}
	}

	// Single-tag: both nodes match.
	resp := get(t, c, "/graph/data?tag="+tagA)
	defer resp.Body.Close()
	var single views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
		t.Fatalf("decode single: %v", err)
	}
	singleByID := make(map[string]views.ArgumentGraphNode, len(single.Nodes))
	for _, n := range single.Nodes {
		singleByID[n.ID] = n
	}
	if _, ok := singleByID[both.ID.String()]; !ok {
		t.Fatalf("single-tag should include both.ID; got %+v", single.Nodes)
	}
	if _, ok := singleByID[onlyA.ID.String()]; !ok {
		t.Fatalf("single-tag should include onlyA.ID; got %+v", single.Nodes)
	}

	// Multi-tag intersects: only `both` should remain a seed.
	resp2 := get(t, c, "/graph/data?tag="+tagA+"&tag="+tagB)
	defer resp2.Body.Close()
	var multi views.ArgumentGraphData
	if err := json.NewDecoder(resp2.Body).Decode(&multi); err != nil {
		t.Fatalf("decode multi: %v", err)
	}
	multiByID := make(map[string]views.ArgumentGraphNode, len(multi.Nodes))
	for _, n := range multi.Nodes {
		multiByID[n.ID] = n
	}
	got, ok := multiByID[both.ID.String()]
	if !ok || !got.Match {
		t.Fatalf("multi-tag should seed `both`; got %+v", multi.Nodes)
	}
	if seed, ok := multiByID[onlyA.ID.String()]; ok && seed.Match {
		t.Fatalf("multi-tag should not seed onlyA (missing tagB); got %+v", seed)
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

// TestArgumentGraphData_HidesCommentsOnHiddenParent pins the rule that a
// comment's visibility in the graph is intersected with its parent
// node's visibility. The cascade trigger copies the parent's visibility
// kind onto the comment row, but node_local_visible_to evaluates the
// "connections" branch against the comment row's own created_by — i.e.
// the commenter, not the parent author. Without node_visible_to also
// walking the comments_on edge, a viewer mutual-followers with the
// commenter (but not with the parent author) used to see the comment
// hanging in the graph without its target.
func TestArgumentGraphData_HidesCommentsOnHiddenParent(t *testing.T) {
	integrationDB(t)

	authorClient := newClient(t)
	author := signupAndGetUser(t, authorClient, "graphvisauthor", "graphvisauthor@example.com", "correct horse battery staple")

	commenterClient := newClient(t)
	commenter := signupAndGetUser(t, commenterClient, "graphviscommenter", "graphviscommenter@example.com", "correct horse battery staple")

	viewerClient := newClient(t)
	viewer := signupAndGetUser(t, viewerClient, "graphvisviewer", "graphvisviewer@example.com", "correct horse battery staple")

	// author <-> commenter mutual follow (so commenter can see the
	// connections-only post and comment on it).
	for _, pair := range [][2]uuid.UUID{
		{author.ID, commenter.ID},
		{commenter.ID, author.ID},
		// viewer <-> commenter mutual follow (the loophole the bug
		// relied on: viewer is "connected to the commenter" but NOT to
		// the parent author).
		{viewer.ID, commenter.ID},
		{commenter.ID, viewer.ID},
	} {
		if err := testServer.queries.CreateFollow(t.Context(), db.CreateFollowParams{
			FollowerID: pair[0],
			FollowedID: pair[1],
		}); err != nil {
			t.Fatalf("follow %s -> %s: %v", pair[0], pair[1], err)
		}
	}

	parentTitle := "Hidden parent " + uuid.NewString()
	parent, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:       db.NodeTypeTopic,
		Title:      parentTitle,
		Body:       "",
		CreatedBy:  author.ID,
		Slug:       "hidden-parent-" + uuid.NewString()[:8],
		Visibility: db.VisibilityKindConnections,
	})
	if err != nil {
		t.Fatalf("create connections node: %v", err)
	}

	commentBody := "Visible-to-commenter-only " + uuid.NewString()
	commentID := uuid.New()
	comment, err := testServer.queries.CreateCommentNode(t.Context(), db.CreateCommentNodeParams{
		NodeID:      parent.ID,
		AuthorID:    commenter.ID,
		ParentID:    nil,
		CommentID:   commentID,
		Body:        commentBody,
		CommentSlug: commentSlug(commentID),
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// Sanity: the commenter (mutual with author) can see the parent.
	resp := get(t, commenterClient, "/graph/data")
	defer resp.Body.Close()
	var commenterData views.ArgumentGraphData
	if err := json.NewDecoder(resp.Body).Decode(&commenterData); err != nil {
		t.Fatalf("decode commenter graph data: %v", err)
	}
	commenterIDs := make(map[string]struct{}, len(commenterData.Nodes))
	for _, n := range commenterData.Nodes {
		commenterIDs[n.ID] = struct{}{}
	}
	if _, ok := commenterIDs[parent.ID.String()]; !ok {
		t.Fatalf("commenter should see parent node; nodes=%+v", commenterData.Nodes)
	}
	if _, ok := commenterIDs[comment.ID.String()]; !ok {
		t.Fatalf("commenter should see their own comment; nodes=%+v", commenterData.Nodes)
	}

	// The actual regression: viewer is connected to the commenter but
	// NOT to the parent author. They must see neither node.
	resp2 := get(t, viewerClient, "/graph/data")
	defer resp2.Body.Close()
	var viewerData views.ArgumentGraphData
	if err := json.NewDecoder(resp2.Body).Decode(&viewerData); err != nil {
		t.Fatalf("decode viewer graph data: %v", err)
	}
	for _, n := range viewerData.Nodes {
		if n.ID == parent.ID.String() {
			t.Fatalf("viewer should not see hidden parent; node=%+v", n)
		}
		if n.ID == comment.ID.String() {
			t.Fatalf("viewer should not see comment on hidden parent; node=%+v", n)
		}
	}
}
