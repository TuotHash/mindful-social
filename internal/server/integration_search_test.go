package server

import (
	"net/url"
	"strings"
	"testing"
)

func TestSearch_ExactWordMatchesViaTsvector(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	createNode(t, c, "view", "Nuclear power is good", "Body about reactors and policy.")
	createNode(t, c, "view", "Solar panels everywhere", "Body about photovoltaics.")

	resp := get(t, c, "/search?"+url.Values{"q": {"nuclear"}}.Encode())
	body := readBody(t, resp)
	if !strings.Contains(body, "Nuclear power is good") {
		t.Fatalf("/search should return tsvector match for 'nuclear'; body: %s", snippet(body))
	}
	if strings.Contains(body, "Solar panels everywhere") {
		t.Fatalf("/search should not return unrelated 'Solar panels'; body: %s", snippet(body))
	}
}

func TestSearch_FuzzyMatchTypo(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	createNode(t, c, "view", "Nuclear power is good", "")
	createNode(t, c, "view", "Solar panels everywhere", "")

	// Typo with missing letter — tsvector misses, trigram fallback catches.
	resp := get(t, c, "/search?"+url.Values{"q": {"nucear"}}.Encode())
	body := readBody(t, resp)
	if !strings.Contains(body, "Nuclear power is good") {
		t.Fatalf("/search should fuzzy-match 'nucear' to 'Nuclear power'; body: %s", snippet(body))
	}
	if strings.Contains(body, "Solar panels everywhere") {
		t.Fatalf("/search should not return unrelated 'Solar panels'; body: %s", snippet(body))
	}
}

func TestSearch_BodyOnlyMatchStillWorks(t *testing.T) {
	// Cross-check: tsvector still indexes the body, so a query that only
	// appears in the body keeps working even though trigrams only see title.
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	createNode(t, c, "view", "Some unrelated title", "Body mentioning photosynthesis at length.")

	resp := get(t, c, "/search?"+url.Values{"q": {"photosynthesis"}}.Encode())
	body := readBody(t, resp)
	if !strings.Contains(body, "Some unrelated title") {
		t.Fatalf("/search should match body-only term; body: %s", snippet(body))
	}
}
