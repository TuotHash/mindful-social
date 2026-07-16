package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completionsServer stands in for an OpenAI-compatible endpoint. It returns
// the assistant contents from bodies one at a time, so a test can script a
// first (bad) response followed by a good one to exercise the retry path.
func completionsServer(t *testing.T, contents ...string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		i := calls
		if i >= len(contents) {
			i = len(contents) - 1
		}
		calls++
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message chatMessage `json:"message"`
		}{Message: chatMessage{Role: "assistant", Content: contents[i]}})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestGenerateNodeHappyPath(t *testing.T) {
	srv, calls := completionsServer(t, `{"type":"topic","title":"Should cities cap rent?","body":"A debate on rent control."}`)
	c := NewClient(srv.URL, "test-model", "")

	draft, err := c.GenerateNode(context.Background(), "rent control debate")
	if err != nil {
		t.Fatalf("GenerateNode: %v", err)
	}
	if draft.Type != "topic" {
		t.Errorf("type = %q, want topic", draft.Type)
	}
	if draft.Title != "Should cities cap rent?" {
		t.Errorf("title = %q", draft.Title)
	}
	if draft.Body != "A debate on rent control." {
		t.Errorf("body = %q", draft.Body)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
}

func TestGenerateNodeRetriesOnMalformedJSON(t *testing.T) {
	// First reply is not JSON; second is valid. The client should retry once
	// and succeed.
	srv, calls := completionsServer(t,
		"here you go: not json at all",
		`{"type":"view","title":"Rent caps help tenants","body":""}`,
	)
	c := NewClient(srv.URL, "test-model", "")

	draft, err := c.GenerateNode(context.Background(), "pro rent-cap opinion")
	if err != nil {
		t.Fatalf("GenerateNode: %v", err)
	}
	if draft.Type != "view" {
		t.Errorf("type = %q, want view", draft.Type)
	}
	if *calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", *calls)
	}
}

func TestGenerateNodeRejectsBadType(t *testing.T) {
	// Both replies name a type outside the allowed set, so after the retry
	// the call fails.
	srv, calls := completionsServer(t, `{"type":"comment","title":"Nope","body":""}`)
	c := NewClient(srv.URL, "test-model", "")

	if _, err := c.GenerateNode(context.Background(), "x"); err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
	if *calls != 2 {
		t.Errorf("calls = %d, want 2 (initial + one retry)", *calls)
	}
}

func TestGenerateNodeStripsJSONFence(t *testing.T) {
	srv, _ := completionsServer(t, "```json\n{\"type\":\"finding\",\"title\":\"Vacancy rose 3%\",\"body\":\"Per the 2024 report.\"}\n```")
	c := NewClient(srv.URL, "test-model", "")

	draft, err := c.GenerateNode(context.Background(), "evidence about vacancies")
	if err != nil {
		t.Fatalf("GenerateNode: %v", err)
	}
	if draft.Type != "finding" || draft.Title != "Vacancy rose 3%" {
		t.Errorf("unexpected draft %+v", draft)
	}
}

func TestGenerateNodeTruncatesLongTitle(t *testing.T) {
	long := strings.Repeat("a", 250)
	srv, _ := completionsServer(t, `{"type":"topic","title":"`+long+`","body":""}`)
	c := NewClient(srv.URL, "test-model", "")

	draft, err := c.GenerateNode(context.Background(), "x")
	if err != nil {
		t.Fatalf("GenerateNode: %v", err)
	}
	if len([]rune(draft.Title)) != maxTitleLen {
		t.Errorf("title len = %d, want %d", len([]rune(draft.Title)), maxTitleLen)
	}
}

func TestGenerateNodeSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "missing", "")

	_, err := c.GenerateNode(context.Background(), "x")
	if err == nil {
		t.Fatal("expected HTTP error, got nil")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error should include endpoint message, got %v", err)
	}
}
