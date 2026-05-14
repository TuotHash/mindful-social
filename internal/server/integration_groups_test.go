package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/db"
)

func TestGroups_PublicGroupCanBeJoined(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")

	resp := formPost(t, alice, "/groups", url.Values{
		"name":        {"Research Circle"},
		"description": {"A shared place for research."},
		"visibility":  {"public"},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create group: status %d body=%s", resp.StatusCode, body)
	}
	if !strings.HasSuffix(resp.Request.URL.Path, "/groups/research-circle") {
		t.Fatalf("create group: expected group redirect, got %s", resp.Request.URL.Path)
	}
	if !strings.Contains(body, "Research Circle") {
		t.Fatalf("group detail missing name; excerpt: %s", snippet(body))
	}

	bob := newClient(t)
	signup(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	body = readBody(t, get(t, bob, "/groups/research-circle"))
	if !strings.Contains(body, ">Join<") {
		t.Fatalf("public group should show join button to non-member; excerpt: %s", snippet(body))
	}
	resp = formPost(t, bob, "/groups/research-circle/join", url.Values{})
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join group: status %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, ">member<") {
		t.Fatalf("joined group should show member badge; excerpt: %s", snippet(body))
	}
}

func TestNodeVisibility_ChildPublicInheritsGroupParent(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")

	group, err := testServer.queries.CreateGroup(t.Context(), db.CreateGroupParams{
		Slug:        "private-research",
		Name:        "Private Research",
		Description: "",
		OwnerID:     aliceUser.ID,
		Visibility:  db.GroupVisibilityKindInvite,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  aliceUser.ID,
		Role:    db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("add owner membership: %v", err)
	}

	resp := formPost(t, alice, "/nodes", url.Values{
		"type":       {"topic"},
		"title":      {"Group-only parent topic"},
		"visibility": {"group:" + group.ID.String()},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create parent topic: status %d body=%s", resp.StatusCode, body)
	}
	parent, err := testServer.queries.GetNodeBySlug(t.Context(), "group-only-parent-topic")
	if err != nil {
		t.Fatalf("lookup parent: %v", err)
	}

	resp = formPost(t, alice, "/nodes", url.Values{
		"type":            {"view"},
		"title":           {"Public child should inherit"},
		"visibility":      {"public"},
		"parent_topic_id": {parent.ID.String()},
	})
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create child view: status %d body=%s", resp.StatusCode, body)
	}
	child, err := testServer.queries.GetNodeBySlug(t.Context(), "public-child-should-inherit")
	if err != nil {
		t.Fatalf("lookup child: %v", err)
	}
	if child.ParentNodeID == nil || *child.ParentNodeID != parent.ID {
		t.Fatalf("child parent_node_id = %v, want %s", child.ParentNodeID, parent.ID)
	}
	if child.GroupID == nil || *child.GroupID != group.ID {
		t.Fatalf("child group_id = %v, want inherited group %s", child.GroupID, group.ID)
	}

	resp = get(t, alice, "/nodes/"+child.Slug)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("group member should see child, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	bob := newClient(t)
	signup(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	resp = get(t, bob, "/nodes/"+child.Slug)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member should not see child inheriting group parent, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	anon := newClient(t)
	resp = get(t, anon, "/nodes/"+child.Slug)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous visitor should not see child inheriting group parent, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
