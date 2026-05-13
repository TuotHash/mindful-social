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

func TestProfilePinsRespectNodeVisibility(t *testing.T) {
	integrationDB(t)
	aliceClient := newClient(t)
	alice := signupAndGetUser(t, aliceClient, "alice", "alice@example.com", "correct horse battery staple")

	publicNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to everyone", db.VisibilityKindPublic, nil)
	privateNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible only to Alice", db.VisibilityKindPrivate, nil)
	connectionsNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to connections", db.VisibilityKindConnections, nil)
	trusted, err := testServer.queries.GetTrustedList(t.Context(), alice.ID)
	if err != nil {
		t.Fatalf("get trusted list: %v", err)
	}
	listNode := createNodeForUser(t, alice.ID, db.NodeTypeView, "Pin visible to list members", db.VisibilityKindList, &trusted.ID)
	for _, nodeID := range []uuid.UUID{publicNode, privateNode, connectionsNode, listNode} {
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
	listClient := newClient(t)
	listMember := signupAndGetUser(t, listClient, "dave", "dave@example.com", "correct horse battery staple")

	for _, follow := range []db.CreateFollowParams{
		{FollowerID: connection.ID, FollowedID: alice.ID},
		{FollowerID: alice.ID, FollowedID: connection.ID},
	} {
		if err := testServer.queries.CreateFollow(t.Context(), follow); err != nil {
			t.Fatalf("create follow: %v", err)
		}
	}
	if err := testServer.queries.AddListMember(t.Context(), db.AddListMemberParams{
		ListID:       trusted.ID,
		MemberUserID: listMember.ID,
	}); err != nil {
		t.Fatalf("add list member: %v", err)
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
			hide: []string{"Pin visible only to Alice", "Pin visible to connections", "Pin visible to list members"},
		},
		{
			name: "unrelated",
			c:    unrelatedClient,
			want: []string{"Pin visible to everyone"},
			hide: []string{"Pin visible only to Alice", "Pin visible to connections", "Pin visible to list members"},
		},
		{
			name: "connection",
			c:    connectionClient,
			want: []string{"Pin visible to everyone", "Pin visible to connections"},
			hide: []string{"Pin visible only to Alice", "Pin visible to list members"},
		},
		{
			name: "list member",
			c:    listClient,
			want: []string{"Pin visible to everyone", "Pin visible to list members"},
			hide: []string{"Pin visible only to Alice", "Pin visible to connections"},
		},
		{
			name: "self",
			c:    aliceClient,
			want: []string{"Pin visible to everyone", "Pin visible only to Alice", "Pin visible to connections", "Pin visible to list members"},
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

func TestProfilePinReasoningsRespectVisibility(t *testing.T) {
	integrationDB(t)
	aliceClient := newClient(t)
	alice := signupAndGetUser(t, aliceClient, "alice", "alice@example.com", "correct horse battery staple")
	view := createNodeForUser(t, alice.ID, db.NodeTypeView, "Public pinned view", db.VisibilityKindPublic, nil)
	privateReasoning := createNodeForUser(t, alice.ID, db.NodeTypeReasoning, "Private pin rationale", db.VisibilityKindPrivate, nil)

	pinID, err := testServer.queries.SetPin(t.Context(), db.SetPinParams{
		UserID: alice.ID,
		NodeID: view,
		Kind:   db.PinKindSupports,
	})
	if err != nil {
		t.Fatalf("set pin: %v", err)
	}
	if err := testServer.queries.AddPinReasoning(t.Context(), db.AddPinReasoningParams{
		PinID:       pinID,
		ReasoningID: privateReasoning,
	}); err != nil {
		t.Fatalf("add pin reasoning: %v", err)
	}

	anonymousBody := readBody(t, get(t, newClient(t), "/users/alice"))
	if !strings.Contains(anonymousBody, "Public pinned view") {
		t.Fatalf("anonymous profile should include public pinned node; excerpt: %s", snippet(anonymousBody))
	}
	if strings.Contains(anonymousBody, "Private pin rationale") {
		t.Fatalf("anonymous profile should hide private pin reasoning; excerpt: %s", snippet(anonymousBody))
	}

	selfBody := readBody(t, get(t, aliceClient, "/users/alice"))
	if !strings.Contains(selfBody, "Private pin rationale") {
		t.Fatalf("self profile should include private pin reasoning; excerpt: %s", snippet(selfBody))
	}
}

func createNodeForUser(t *testing.T, userID uuid.UUID, nodeType db.NodeType, title string, visibility db.VisibilityKind, listID *uuid.UUID) uuid.UUID {
	t.Helper()
	node, err := testServer.queries.CreateNode(t.Context(), db.CreateNodeParams{
		Type:             nodeType,
		Title:            title,
		Body:             "",
		CreatedBy:        userID,
		Slug:             "test-" + uuid.NewString()[:8],
		Visibility:       visibility,
		VisibilityListID: listID,
	})
	if err != nil {
		t.Fatalf("create node %q: %v", title, err)
	}
	return node.ID
}
