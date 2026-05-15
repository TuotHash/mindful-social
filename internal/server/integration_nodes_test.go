package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/db"
)

func TestNodeCreate_RendersOnDetail(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Open source is good", "Pro-FOSS stance.")

	body := readBody(t, get(t, c, "/nodes/"+id.String()))
	if !strings.Contains(body, "Open source is good") {
		t.Fatalf("detail page missing title; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Pro-FOSS stance.") {
		t.Fatalf("detail page missing body; excerpt: %s", snippet(body))
	}
}

func TestNodeCreate_RendersMarkdownBodySafely(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Markdown body", "**important**\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(1))")

	body := readBody(t, get(t, c, "/nodes/"+id.String()))
	if !strings.Contains(body, "<strong>important</strong>") {
		t.Fatalf("detail page missing rendered markdown; excerpt: %s", snippet(body))
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "<script>alert") || strings.Contains(lower, `href="javascript:`) {
		t.Fatalf("detail page rendered unsafe markdown; excerpt: %s", snippet(body))
	}
}

func TestNodeDelete_AuthorOnly(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, alice, "topic", "A topic", "")

	bob := newClient(t)
	signup(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	resp := formPost(t, bob, "/nodes/"+id.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author delete: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Node still exists.
	resp = get(t, alice, "/nodes/"+id.String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("node still expected to exist, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Author can delete.
	resp = formPost(t, alice, "/nodes/"+id.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author delete: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Detail page now 404s.
	resp = get(t, alice, "/nodes/"+id.String())
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestNodeCreate_FindingRequiresParent(t *testing.T) {
	// Findings carry a finding_requires_parent DB constraint and the Post
	// form mirrors it: a finding submission without a selected parent is
	// re-rendered with a flash, not created.
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	resp := formPost(t, c, "/nodes", url.Values{
		"type":  {"finding"},
		"title": {"Should not create"},
	})
	body := readBody(t, resp)
	if !strings.Contains(body, "finding must attach") {
		t.Fatalf("expected flash about parent requirement, got: %s", snippet(body))
	}
	if strings.HasPrefix(resp.Request.URL.Path, "/nodes/") && resp.Request.URL.Path != "/nodes" {
		t.Fatalf("unexpected redirect to %s", resp.Request.URL.Path)
	}
}

func TestNodeCreate_FindingAttachesToParentWithEdge(t *testing.T) {
	// Submitting type=finding with a parent and edge kind creates the node
	// AND a parent→finding edge of that kind, matching the inline-edge flow.
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	parentID := createNode(t, c, "view", "Anchor view", "")

	resp := formPost(t, c, "/nodes", url.Values{
		"type":              {"finding"},
		"title":             {"Citation finding"},
		"body":              {""},
		"parent_node_id":    {parentID.String()},
		"finding_edge_kind": {"supports"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create finding: status %d", resp.StatusCode)
	}
	parts := strings.Split(strings.TrimPrefix(resp.Request.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "nodes" {
		t.Fatalf("expected redirect to /nodes/{slug}, got %s", resp.Request.URL.Path)
	}
	finding, err := testServer.queries.GetNodeBySlug(t.Context(), parts[1])
	if err != nil {
		t.Fatalf("lookup finding: %v", err)
	}
	if finding.Type != db.NodeTypeFinding {
		t.Fatalf("expected finding type, got %s", finding.Type)
	}
	if finding.ParentNodeID == nil || *finding.ParentNodeID != parentID {
		t.Fatalf("expected parent_node_id=%s, got %v", parentID, finding.ParentNodeID)
	}
	edges, err := testServer.queries.ListEdgesFromNodeForViewer(t.Context(), db.ListEdgesFromNodeForViewerParams{
		FromNode: parentID,
		ViewerID: &finding.CreatedBy,
	})
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.ToID == finding.ID && e.Kind == db.EdgeKindSupports {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parent→finding edge of kind supports; edges=%+v", edges)
	}
}

func TestNodeUpdate_AdminCanChangeVisibility(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, alice, "view", "Public take", "Body")
	node, err := testServer.queries.GetNode(t.Context(), id)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if node.Visibility != db.VisibilityKindPublic {
		t.Fatalf("seed: expected public visibility, got %q", node.Visibility)
	}
	_ = aliceUser

	mod := newClient(t)
	modUser := signupAndGetUser(t, mod, "mod", "mod@example.com", "correct horse battery staple")
	if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
		ID:   modUser.ID,
		Role: db.UserRoleAdmin,
	}); err != nil {
		t.Fatalf("promote mod: %v", err)
	}

	// After the update, the admin is redirected to /nodes/<slug>, which
	// 404s because the node is now private and the admin isn't the
	// author — that's correct visibility behaviour, not an update
	// failure. Verify the change at the DB layer.
	resp := formPost(t, mod, "/nodes/"+node.Slug, url.Values{
		"title":      {node.Title},
		"body":       {node.Body},
		"visibility": {"private"},
	})
	resp.Body.Close()

	got, err := testServer.queries.GetNode(t.Context(), id)
	if err != nil {
		t.Fatalf("get node after update: %v", err)
	}
	if got.Visibility != db.VisibilityKindPrivate {
		t.Fatalf("visibility = %q, want private", got.Visibility)
	}
	// Author-only fields must remain untouched even though the admin's POST
	// did not include edit_policy / link_policy.
	if got.EditPolicy != node.EditPolicy {
		t.Fatalf("edit_policy mutated: was %q, now %q", node.EditPolicy, got.EditPolicy)
	}
	if got.LinkPolicy != node.LinkPolicy {
		t.Fatalf("link_policy mutated: was %q, now %q", node.LinkPolicy, got.LinkPolicy)
	}
}

func TestNodeDelete_AdminCanDeleteAnyNode(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, alice, "topic", "Alice's topic", "")

	mod := newClient(t)
	modUser := signupAndGetUser(t, mod, "mod", "mod@example.com", "correct horse battery staple")
	if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
		ID:   modUser.ID,
		Role: db.UserRoleAdmin,
	}); err != nil {
		t.Fatalf("promote mod: %v", err)
	}

	resp := formPost(t, mod, "/nodes/"+id.String()+"/delete", url.Values{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin delete: status %d", resp.StatusCode)
	}

	resp = get(t, alice, "/nodes/"+id.String())
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after admin delete: expected 404, got %d", resp.StatusCode)
	}
}

func TestNodeUpdate_HTMXRequestsGetFullRedirect(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Original title", "Original body")
	node, err := testServer.queries.GetNode(t.Context(), id)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}

	form := url.Values{
		"title": {"Updated title"},
		"body":  {"Updated body"},
	}
	form.Set("gorilla.csrf.Token", fetchCSRFToken(t, c))
	req, err := http.NewRequest(http.MethodPost, testTS.URL+"/nodes/"+node.Slug, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST update: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("htmx update status = %d, want 200", resp.StatusCode)
	}
	if got, want := resp.Header.Get("HX-Redirect"), "/nodes/"+node.Slug; got != want {
		t.Fatalf("HX-Redirect = %q, want %q", got, want)
	}
}
