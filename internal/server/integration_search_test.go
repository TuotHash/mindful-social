package server

import (
	"net/http"
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

// TestSearch_FindsPublicGroupByName covers the new Groups section on
// /search: a public group's name turns up for any signed-out or signed-in
// viewer via the trigram + ILIKE branches in SearchGroups.
func TestSearch_FindsPublicGroupByName(t *testing.T) {
	integrationDB(t)
	owner := newClient(t)
	signup(t, owner, "alicegrp", "alicegrp@example.com", "correct horse battery staple")

	resp := formPost(t, owner, "/groups", url.Values{
		"name":        {"Renewable Reactors Coalition"},
		"description": {"A coalition for renewable reactors."},
		"visibility":  {"public"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create public group: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	visitor := newClient(t)
	body := readBody(t, get(t, visitor, "/search?"+url.Values{"q": {"renewable"}}.Encode()))
	if !strings.Contains(body, "Groups") {
		t.Fatalf("/search should render a Groups section; body: %s", snippet(body))
	}
	if !strings.Contains(body, `href="/groups/renewable-reactors-coalition"`) {
		t.Fatalf("/search should link matching group; body: %s", snippet(body))
	}
}

// TestSearch_HidesPrivateGroupsFromOutsiders pins the visibility gate on
// the new SearchGroups query: a private group's name must not surface in
// search results for a non-member, even with a perfect-match query.
func TestSearch_HidesPrivateGroupsFromOutsiders(t *testing.T) {
	integrationDB(t)
	owner := newClient(t)
	signup(t, owner, "aliceprv", "aliceprv@example.com", "correct horse battery staple")
	resp := formPost(t, owner, "/groups", url.Values{
		"name":       {"Quiet Backroom"},
		"visibility": {"private"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create private group: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	stranger := newClient(t)
	signup(t, stranger, "stranger", "stranger@example.com", "correct horse battery staple")
	body := readBody(t, get(t, stranger, "/search?"+url.Values{"q": {"quiet backroom"}}.Encode()))
	if strings.Contains(body, "Quiet Backroom") {
		t.Fatalf("/search should not surface a private group to a non-member; body: %s", snippet(body))
	}

	// Owner sees their own group, regardless of visibility.
	body = readBody(t, get(t, owner, "/search?"+url.Values{"q": {"quiet backroom"}}.Encode()))
	if !strings.Contains(body, "Quiet Backroom") {
		t.Fatalf("/search should surface the owner's own private group; body: %s", snippet(body))
	}
}

func TestSearch_FindsPeopleByUsername(t *testing.T) {
	integrationDB(t)
	aliceClient := newClient(t)
	signup(t, aliceClient, "alice", "alice@example.com", "correct horse battery staple")
	bobClient := newClient(t)
	signup(t, bobClient, "bob-builder", "bob@example.com", "correct horse battery staple")

	resp := get(t, aliceClient, "/search?"+url.Values{"q": {"builder"}}.Encode())
	body := readBody(t, resp)
	if !strings.Contains(body, "People") {
		t.Fatalf("/search should render a People section; body: %s", snippet(body))
	}
	if !strings.Contains(body, `href="/users/bob-builder"`) {
		t.Fatalf("/search should link matching user profile; body: %s", snippet(body))
	}
}
