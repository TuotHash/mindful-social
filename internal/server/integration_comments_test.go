package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCommentCreate_RendersOnViewOnly(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	viewID := createNode(t, c, "view", "Commentable view", "View body")

	resp := formPost(t, c, "/nodes/"+viewID.String()+"/comments", url.Values{
		"body": {"First plain-text comment."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create comment: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body := readBody(t, get(t, c, "/nodes/"+viewID.String()))
	if !strings.Contains(body, "First plain-text comment.") {
		t.Fatalf("detail page missing comment; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "1 comment") {
		t.Fatalf("detail page missing comment count; excerpt: %s", snippet(body))
	}

	topicID := createNode(t, c, "topic", "Not a comment thread", "")
	resp = formPost(t, c, "/nodes/"+topicID.String()+"/comments", url.Values{
		"body": {"Should fail"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("comment on topic: status %d, want 400", resp.StatusCode)
	}
}

func TestCommentReplyRejectsReplyToReply(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	viewID := createNode(t, c, "view", "Two-level only", "")

	resp := formPost(t, c, "/nodes/"+viewID.String()+"/comments", url.Values{
		"body": {"Top-level"},
	})
	resp.Body.Close()
	topID := onlyTopLevelComment(t, viewID)

	resp = formPost(t, c, "/nodes/"+viewID.String()+"/comments", url.Values{
		"parent_id": {topID.String()},
		"body":      {"Allowed reply"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reply to top-level: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	replyID := onlyReplyComment(t, viewID)

	resp = formPost(t, c, "/nodes/"+viewID.String()+"/comments", url.Values{
		"parent_id": {replyID.String()},
		"body":      {"Nested reply should fail"},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reply to reply: status %d, want 400; body=%s", resp.StatusCode, snippet(body))
	}
	if !strings.Contains(body, "invalid comment target") {
		t.Fatalf("reply to reply: expected invalid target error, got %s", snippet(body))
	}

	rows, err := testServer.queries.ListCommentsForNode(t.Context(), viewID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("nested reply should not create a row; got %d comments", len(rows))
	}
}

func TestCommentEditAndSoftDelete_AuthorOnly(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	viewID := createNode(t, alice, "view", "Comment ownership", "")

	resp := formPost(t, alice, "/nodes/"+viewID.String()+"/comments", url.Values{
		"body": {"Original comment"},
	})
	resp.Body.Close()
	commentID := onlyTopLevelComment(t, viewID)

	bob := newClient(t)
	signup(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	resp = get(t, bob, "/nodes/"+viewID.String()+"/comments/"+commentID.String()+"/edit")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author edit form: status %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
	resp = formPost(t, bob, "/nodes/"+viewID.String()+"/comments/"+commentID.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author delete: status %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()

	resp = formPost(t, alice, "/nodes/"+viewID.String()+"/comments/"+commentID.String(), url.Values{
		"body": {"Updated comment"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author edit: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	body := readBody(t, get(t, alice, "/nodes/"+viewID.String()))
	if !strings.Contains(body, "Updated comment") || !strings.Contains(body, "edited") {
		t.Fatalf("edited comment not rendered correctly; excerpt: %s", snippet(body))
	}

	resp = formPost(t, alice, "/nodes/"+viewID.String()+"/comments/"+commentID.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author soft delete: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	body = readBody(t, get(t, alice, "/nodes/"+viewID.String()))
	if !strings.Contains(body, "comment removed") {
		t.Fatalf("soft-deleted comment slot missing; excerpt: %s", snippet(body))
	}
	if strings.Contains(body, "Updated comment") {
		t.Fatalf("soft-deleted comment body should be hidden; excerpt: %s", snippet(body))
	}
}

func TestTopicPageListsChildViewsWithCommentCounts(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "Topic feed", "")
	viewID := createViewUnderTopic(t, c, topicID, "A child view", "A short body for the topic feed.")

	resp := formPost(t, c, "/nodes/"+viewID.String()+"/comments", url.Values{
		"body": {"Feed count comment"},
	})
	resp.Body.Close()

	body := readBody(t, get(t, c, "/nodes/"+topicID.String()))
	for _, want := range []string{"A child view", "alice", "1 comment", "A short body for the topic feed."} {
		if !strings.Contains(body, want) {
			t.Fatalf("topic feed missing %q; excerpt: %s", want, snippet(body))
		}
	}
}

func onlyTopLevelComment(t *testing.T, nodeID uuid.UUID) uuid.UUID {
	t.Helper()
	rows, err := testServer.queries.ListCommentsForNode(t.Context(), nodeID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	for _, row := range rows {
		if row.ParentID == nil {
			return row.ID
		}
	}
	t.Fatal("expected a top-level comment")
	return uuid.Nil
}

func onlyReplyComment(t *testing.T, nodeID uuid.UUID) uuid.UUID {
	t.Helper()
	rows, err := testServer.queries.ListCommentsForNode(t.Context(), nodeID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	for _, row := range rows {
		if row.ParentID != nil {
			return row.ID
		}
	}
	t.Fatal("expected a reply comment")
	return uuid.Nil
}

func createViewUnderTopic(t *testing.T, c *http.Client, topicID uuid.UUID, title, body string) uuid.UUID {
	t.Helper()
	resp := formPost(t, c, "/nodes", url.Values{
		"type":            {"view"},
		"title":           {title},
		"body":            {body},
		"parent_topic_id": {topicID.String()},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create child view: status %d", resp.StatusCode)
	}
	parts := strings.Split(strings.TrimPrefix(resp.Request.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "nodes" {
		t.Fatalf("create child view: unexpected redirect to %s", resp.Request.URL.Path)
	}
	node, err := testServer.queries.GetNodeBySlug(t.Context(), parts[1])
	if err != nil {
		t.Fatalf("create child view: lookup by slug %q: %v", parts[1], err)
	}
	return node.ID
}
