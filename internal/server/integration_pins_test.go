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

	// Detail page reflects the current kind: the Oppose button is pressed
	// and the meter shows a 100% oppose share (alice is the only voter).
	body := readBody(t, get(t, c, "/nodes/"+view.String()))
	if !strings.Contains(body, "stance-opposes pressed") {
		t.Fatalf("detail page should mark Oppose as pressed; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "100% oppose") {
		t.Fatalf("detail page should show 100%% oppose in meter; excerpt: %s", snippet(body))
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

func TestProfilePinsRespectNodeVisibility(t *testing.T) {
	integrationDB(t)
	aliceClient := newClient(t)
	alice := signupAndGetUser(t, aliceClient, "alice", "alice@example.com", "correct horse battery staple")

	publicNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to everyone", db.VisibilityKindPublic, nil)
	privateNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible only to Alice", db.VisibilityKindPrivate, nil)
	connectionsNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to connections", db.VisibilityKindConnections, nil)
	group, err := testServer.queries.CreateGroup(t.Context(), db.CreateGroupParams{
		Slug:        "test-pins-" + uuid.NewString()[:8],
		Name:        "Profile pin audience",
		Description: "",
		OwnerID:     alice.ID,
		Visibility:  db.GroupVisibilityKindPrivate,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  alice.ID,
		Role:    db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("add owner to group: %v", err)
	}
	groupNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to group members", db.VisibilityKindGroup, &group.ID)
	for _, nodeID := range []uuid.UUID{publicNode, privateNode, connectionsNode, groupNode} {
		if _, err := testServer.queries.SetPin(t.Context(), db.SetPinParams{
			UserID: alice.ID,
			NodeID: nodeID,
			Kind:   db.PinKindSupports,
		}); err != nil {
			t.Fatalf("set pin: %v", err)
		}
	}

	unrelatedClient := newClient(t)
	signup(t, unrelatedClient, "bob", "bob@example.com", "correct horse battery staple")
	connectionClient := newClient(t)
	connection := signupAndGetUser(t, connectionClient, "carol", "carol@example.com", "correct horse battery staple")
	groupClient := newClient(t)
	groupMember := signupAndGetUser(t, groupClient, "dave", "dave@example.com", "correct horse battery staple")

	for _, follow := range []db.CreateFollowParams{
		{FollowerID: connection.ID, FollowedID: alice.ID},
		{FollowerID: alice.ID, FollowedID: connection.ID},
	} {
		if err := testServer.queries.CreateFollow(t.Context(), follow); err != nil {
			t.Fatalf("create follow: %v", err)
		}
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  groupMember.ID,
		Role:    db.GroupMemberRoleMember,
	}); err != nil {
		t.Fatalf("add group member: %v", err)
	}

	cases := []struct {
		name string
		c    *http.Client
		want []string
		hide []string
	}{
		{
			name: "anonymous",
			c:    newClient(t),
			want: []string{"Pin visible to everyone"},
			hide: []string{"Pin visible only to Alice", "Pin visible to connections", "Pin visible to group members"},
		},
		{
			name: "unrelated",
			c:    unrelatedClient,
			want: []string{"Pin visible to everyone"},
			hide: []string{"Pin visible only to Alice", "Pin visible to connections", "Pin visible to group members"},
		},
		{
			name: "connection",
			c:    connectionClient,
			want: []string{"Pin visible to everyone", "Pin visible to connections"},
			hide: []string{"Pin visible only to Alice", "Pin visible to group members"},
		},
		{
			name: "group member",
			c:    groupClient,
			want: []string{"Pin visible to everyone", "Pin visible to group members"},
			hide: []string{"Pin visible only to Alice", "Pin visible to connections"},
		},
		{
			name: "self",
			c:    aliceClient,
			want: []string{"Pin visible to everyone", "Pin visible only to Alice", "Pin visible to connections", "Pin visible to group members"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := readBody(t, get(t, tc.c, "/users/alice"))
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("profile should include %q; excerpt: %s", want, snippet(body))
				}
			}
			for _, hide := range tc.hide {
				if strings.Contains(body, hide) {
					t.Fatalf("profile should hide %q; excerpt: %s", hide, snippet(body))
				}
			}
		})
	}
}

func createNodeForUser(t *testing.T, userID uuid.UUID, nodeType db.NodeType, title string, visibility db.VisibilityKind, groupID *uuid.UUID) uuid.UUID {
	t.Helper()
	node, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:              nodeType,
		Title:             title,
		Body:              "",
		CreatedBy:         userID,
		Slug:              "test-" + uuid.NewString()[:8],
		Visibility:        visibility,
		VisibilityGroupID: groupID,
	})
	if err != nil {
		t.Fatalf("create node %q: %v", title, err)
	}
	return node.ID
}

// Unpinning a node the user never pinned returns 404 rather than 303 OK.
// Before the :execrows change, the SQL silently succeeded on zero rows and
// the handler reported success.
func TestUnpin_NonexistentReturns404(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	view := createNode(t, c, "view", "View A", "")

	resp := formPost(t, c, "/nodes/"+view.String()+"/unpin", url.Values{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unpin without prior pin: status %d, want 404", resp.StatusCode)
	}
}
