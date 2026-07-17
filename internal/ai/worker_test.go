package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppendSources(t *testing.T) {
	if got := appendSources("body text", nil); got != "body text" {
		t.Errorf("no sources should leave body unchanged, got %q", got)
	}
	out := appendSources("The claim.", []Source{
		{URL: "https://a.example", Title: "A title"},
		{URL: "https://b.example", Title: ""}, // untitled falls back to URL
	})
	if !strings.Contains(out, "The claim.") {
		t.Errorf("original body dropped: %q", out)
	}
	if !strings.Contains(out, "**Sources**") {
		t.Errorf("missing Sources heading: %q", out)
	}
	if !strings.Contains(out, "[A title](https://a.example)") {
		t.Errorf("missing titled source link: %q", out)
	}
	if !strings.Contains(out, "[https://b.example](https://b.example)") {
		t.Errorf("untitled source should fall back to URL: %q", out)
	}
}

func TestMarshalSources(t *testing.T) {
	b, err := marshalSources([]Source{{URL: "https://a", Title: "A", Text: "ignored body"}})
	if err != nil {
		t.Fatalf("marshalSources: %v", err)
	}
	var refs []sourceRef
	if err := json.Unmarshal(b, &refs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(refs) != 1 || refs[0].URL != "https://a" || refs[0].Title != "A" {
		t.Errorf("unexpected refs %+v", refs)
	}
	// Text must not be persisted — only url/title.
	if strings.Contains(string(b), "ignored body") {
		t.Errorf("source text should not be persisted: %s", b)
	}
}
