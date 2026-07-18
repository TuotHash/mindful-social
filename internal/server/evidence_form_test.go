package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseEvidenceForm(t *testing.T) {
	form := url.Values{}
	// Two rendered items; only index 0 is ticked. Hidden source fields are
	// always submitted, so both items are discoverable.
	form.Set("evidence_include", "0")
	form.Set("evidence_source_0", "https://a.example/x")
	form.Set("evidence_title_0", "  Edited title A  ")
	form.Set("evidence_body_0", "why A matters")
	form.Set("evidence_relation_0", "supports")
	form.Set("evidence_source_1", "https://www.b.example/y")
	form.Set("evidence_title_1", "B")
	form.Set("evidence_relation_1", "related")

	r := httptest.NewRequest("POST", "/nodes", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	items := parseEvidenceForm(r)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(items), items)
	}
	if items[0].Index != 0 || items[1].Index != 1 {
		t.Errorf("items not sorted by index: %+v", items)
	}
	if !items[0].Included {
		t.Error("item 0 should be included (ticked)")
	}
	if items[1].Included {
		t.Error("item 1 should not be included (unticked)")
	}
	if items[0].Title != "Edited title A" {
		t.Errorf("item 0 title not trimmed/edited: %q", items[0].Title)
	}
	if items[1].SourceDomain != "b.example" {
		t.Errorf("item 1 domain should strip www: %q", items[1].SourceDomain)
	}
}

func TestEvidenceFromJSON(t *testing.T) {
	raw := []byte(`[{"title":"A","body":"b","source_url":"https://a.example","relation":"supports"}]`)
	items := evidenceFromJSON(raw)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !items[0].Included {
		t.Error("evidence from a completed job should be ticked by default")
	}
	if items[0].SourceDomain != "a.example" {
		t.Errorf("domain = %q", items[0].SourceDomain)
	}
	if evidenceFromJSON(nil) != nil {
		t.Error("nil input should yield nil")
	}
}
