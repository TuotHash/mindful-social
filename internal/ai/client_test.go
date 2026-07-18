package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completionsServer stands in for an OpenAI-compatible streaming endpoint. It
// streams the assistant contents from bodies one at a time (as SSE deltas
// followed by [DONE]), so a test can script a first (bad) response followed by a
// good one to exercise the retry path. Each content is split into a few chunks
// to mimic real token-by-token streaming.
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
		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range splitIntoChunks(contents[i], 3) {
			frame, _ := json.Marshal(streamChunk{Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{{Delta: struct {
				Content string `json:"content"`
			}{Content: delta}}}})
			_, _ = w.Write([]byte("data: " + string(frame) + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// splitIntoChunks divides s into at most n roughly-equal pieces (fewer when s
// is short), so the fake endpoint emits several deltas per completion.
func splitIntoChunks(s string, n int) []string {
	if s == "" {
		return []string{""}
	}
	r := []rune(s)
	if n < 1 {
		n = 1
	}
	if n > len(r) {
		n = len(r)
	}
	size := (len(r) + n - 1) / n
	var out []string
	for i := 0; i < len(r); i += size {
		end := i + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
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

func TestBuildUserContent(t *testing.T) {
	if got := buildUserContent("just a prompt", nil); got != "just a prompt" {
		t.Errorf("no sources should pass the prompt through, got %q", got)
	}
	out := buildUserContent("draft a topic", []Source{
		{URL: "https://x.example", Title: "X", Text: "vacancy rose 3% in 2024"},
	})
	for _, want := range []string{"draft a topic", "https://x.example", "vacancy rose 3% in 2024", "do not invent facts"} {
		if !strings.Contains(out, want) {
			t.Errorf("grounded content missing %q:\n%s", want, out)
		}
	}
}

func TestGenerateNodeGroundedReturnsDraft(t *testing.T) {
	srv, _ := completionsServer(t, `{"type":"finding","title":"Vacancy rose 3%","body":"Per the source."}`)
	c := NewClient(srv.URL, "test-model", "")

	draft, err := c.GenerateNodeGrounded(context.Background(), "vacancies", []Source{
		{URL: "https://x.example", Title: "X", Text: "vacancy rose 3%"},
	})
	if err != nil {
		t.Fatalf("GenerateNodeGrounded: %v", err)
	}
	if draft.Type != "finding" || draft.Title != "Vacancy rose 3%" {
		t.Errorf("unexpected draft %+v", draft)
	}
}

func TestGenerateNodeGroundedStreamForwardsDeltas(t *testing.T) {
	content := `{"type":"topic","title":"Should cities cap rent?","body":"A debate."}`
	srv, _ := completionsServer(t, content)
	c := NewClient(srv.URL, "test-model", "")

	var got strings.Builder
	deltas := 0
	draft, err := c.GenerateNodeGroundedStream(context.Background(), "rent", nil, func(d string) {
		got.WriteString(d)
		deltas++
	})
	if err != nil {
		t.Fatalf("GenerateNodeGroundedStream: %v", err)
	}
	if draft.Type != "topic" || draft.Title != "Should cities cap rent?" {
		t.Errorf("unexpected draft %+v", draft)
	}
	if deltas < 2 {
		t.Errorf("expected multiple deltas, got %d", deltas)
	}
	// The concatenated deltas must reconstruct exactly what was parsed.
	if got.String() != content {
		t.Errorf("reassembled stream = %q, want %q", got.String(), content)
	}
}

func TestParseDraftReadsEvidence(t *testing.T) {
	content := `{"type":"view","title":"Can a narcissist change?","body":"It depends.","evidence":[` +
		`{"title":"7 Steps","body":"Motivated patients can work on behaviors.","source_url":"https://psychologytoday.com/x","relation":"supports"},` +
		`{"title":"Thriveworks","body":"Discusses therapy challenges.","source_url":"https://thriveworks.com/y","relation":"weird"},` +
		`{"title":"","body":"no title","source_url":"https://drop.me"},` +
		`{"title":"No URL","body":"dropped","source_url":""}]}`
	draft, err := parseDraft(content)
	if err != nil {
		t.Fatalf("parseDraft: %v", err)
	}
	if len(draft.Evidence) != 2 {
		t.Fatalf("expected 2 valid evidence items, got %d: %+v", len(draft.Evidence), draft.Evidence)
	}
	if draft.Evidence[0].Relation != "supports" {
		t.Errorf("item 0 relation = %q, want supports", draft.Evidence[0].Relation)
	}
	// Unknown relation falls back to "related".
	if draft.Evidence[1].Relation != "related" {
		t.Errorf("item 1 relation = %q, want related (fallback)", draft.Evidence[1].Relation)
	}
}

func TestParseDraftNoEvidence(t *testing.T) {
	draft, err := parseDraft(`{"type":"topic","title":"A topic","body":""}`)
	if err != nil {
		t.Fatalf("parseDraft: %v", err)
	}
	if len(draft.Evidence) != 0 {
		t.Errorf("expected no evidence, got %+v", draft.Evidence)
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
