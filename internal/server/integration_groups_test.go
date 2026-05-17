package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

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
		Visibility:  db.GroupVisibilityKindConnections,
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
		"type":           {"view"},
		"title":          {"Public child should inherit"},
		"visibility":     {"public"},
		"parent_node_id": {parent.ID.String()},
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

// seedGroupWith creates a group owned by `alice` and adds `bob` with the
// given role. Used by the role / moderation tests below. Returns the
// group's slug and ID.
func seedGroupWith(t *testing.T, alice *http.Client, aliceUser, bobUser db.User, bobRole db.GroupMemberRole, slug string) (string, uuid.UUID) {
	t.Helper()
	group, err := testServer.queries.CreateGroup(t.Context(), db.CreateGroupParams{
		Slug:       slug,
		Name:       slug,
		OwnerID:    aliceUser.ID,
		Visibility: db.GroupVisibilityKindConnections,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID, UserID: aliceUser.ID, Role: db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID, UserID: bobUser.ID, Role: bobRole,
	}); err != nil {
		t.Fatalf("add bob: %v", err)
	}
	_ = alice
	return group.Slug, group.ID
}

func TestGroups_EditorCanEditAndDeleteHostedNode(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	_, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleEditor, "editor-test-group")

	resp := formPost(t, alice, "/nodes", url.Values{
		"type":       {"topic"},
		"title":      {"Alice's group topic"},
		"visibility": {"group:" + groupID.String()},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create group-hosted topic: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	node, err := testServer.queries.GetNodeBySlug(t.Context(), "alice-s-group-topic")
	if err != nil {
		t.Fatalf("lookup hosted node: %v", err)
	}

	// Editor opens the edit page successfully — canEditNode honors group staff.
	resp = get(t, bob, "/nodes/"+node.ID.String()+"/edit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor opening edit page: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Editor deletes the node — canDeleteNode honors group staff too.
	resp = formPost(t, bob, "/nodes/"+node.ID.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor deleting node: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	if _, err := testServer.queries.GetNodeBySlug(t.Context(), node.Slug); err == nil {
		t.Fatalf("expected node to be deleted")
	}
}

func TestGroups_PlainMemberCannotModerate(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	_, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleMember, "member-test-group")

	resp := formPost(t, alice, "/nodes", url.Values{
		"type":       {"topic"},
		"title":      {"Members can't moderate me"},
		"visibility": {"group:" + groupID.String()},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed node: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	node, err := testServer.queries.GetNodeBySlug(t.Context(), "members-can-t-moderate-me")
	if err != nil {
		t.Fatalf("lookup node: %v", err)
	}

	resp = formPost(t, bob, "/nodes/"+node.ID.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("plain member deleting non-author node: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestGroups_AdminCanChangeMemberRoleButNotOwners(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	slug, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleMember, "role-change-group")

	// Owner promotes bob to editor.
	resp := formPost(t, alice, "/groups/"+slug+"/members/"+bobUser.ID.String()+"/role", url.Values{
		"role": {"editor"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set role: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	row, err := testServer.queries.GetGroupMembership(t.Context(), db.GetGroupMembershipParams{
		GroupID: groupID, UserID: bobUser.ID,
	})
	if err != nil {
		t.Fatalf("lookup membership: %v", err)
	}
	if row.Role != db.GroupMemberRoleEditor {
		t.Fatalf("expected editor role, got %s", row.Role)
	}

	// Owner can't be demoted through this endpoint.
	resp = formPost(t, alice, "/groups/"+slug+"/members/"+aliceUser.ID.String()+"/role", url.Values{
		"role": {"member"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set owner role: status %d", resp.StatusCode)
	}
	ownerRow, err := testServer.queries.GetGroupMembership(t.Context(), db.GetGroupMembershipParams{
		GroupID: groupID, UserID: aliceUser.ID,
	})
	if err != nil {
		t.Fatalf("lookup owner membership: %v", err)
	}
	if ownerRow.Role != db.GroupMemberRoleOwner {
		t.Fatalf("owner role should be preserved, got %s", ownerRow.Role)
	}
}

func TestGroups_MemberRoleChangeRequiresManageRights(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	slug, _ := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleMember, "no-self-promote-group")

	// Bob is a plain member; trying to promote himself must 403.
	resp := formPost(t, bob, "/groups/"+slug+"/members/"+bobUser.ID.String()+"/role", url.Values{
		"role": {"admin"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member self-promoting: expected 403, got %d", resp.StatusCode)
	}
}

// TestGroups_ConnectionsVisibilityHonorsMutualFollows pins the gating
// on the new 'connections' visibility: a stranger gets a 404 on the
// detail page (and the group is hidden from the index), a one-way
// follower also gets a 404, but a mutual follower of the owner can
// see and read the group.
func TestGroups_ConnectionsVisibilityHonorsMutualFollows(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "aliceconns", "aliceconns@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bobconns", "bobconns@example.com", "correct horse battery staple")
	stranger := newClient(t)
	signup(t, stranger, "stranger2", "stranger2@example.com", "correct horse battery staple")

	group, err := testServer.queries.CreateGroup(t.Context(), db.CreateGroupParams{
		Slug:       "salon-of-connections",
		Name:       "Salon of Connections",
		OwnerID:    aliceUser.ID,
		Visibility: db.GroupVisibilityKindConnections,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := testServer.queries.AddGroupMember(t.Context(), db.AddGroupMemberParams{
		GroupID: group.ID, UserID: aliceUser.ID, Role: db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	// Stranger has no follow link with alice — page must 404.
	resp := get(t, stranger, "/groups/"+group.Slug)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stranger seeing connections-only group: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Bob follows alice, but she doesn't follow back — still not a
	// connection (mutual), so still 404.
	resp = formPost(t, bob, "/users/"+aliceUser.Username+"/follow", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob follow alice: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = get(t, bob, "/groups/"+group.Slug)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("one-way follower seeing connections-only group: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Alice follows bob back — now they are a mutual connection, so bob
	// can read the page.
	resp = formPost(t, alice, "/users/"+bobUser.Username+"/follow", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alice follow bob: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	body := readBody(t, get(t, bob, "/groups/"+group.Slug))
	if !strings.Contains(body, "Salon of Connections") {
		t.Fatalf("connection should see group page; excerpt: %s", snippet(body))
	}

	// Index also reflects the gating: stranger's /groups page must not
	// list the connections-only group.
	body = readBody(t, get(t, stranger, "/groups"))
	if strings.Contains(body, "Salon of Connections") {
		t.Fatalf("/groups index leaked connections-only group to stranger; excerpt: %s", snippet(body))
	}
}

// TestGroups_AdminCannotChangeVisibility pins the owner-only gate on the
// settings handler: an admin can still save the member-list visibility,
// but their visibility= field is silently ignored. Used to enforce that
// audience-control stays with the owner alone.
func TestGroups_AdminCannotChangeVisibility(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "aliceownr", "aliceownr@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bobadmin", "bobadmin@example.com", "correct horse battery staple")
	slug, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleAdmin, "owner-only-vis")

	// Bob (admin) tries to flip the group to public.
	resp := formPost(t, bob, "/groups/"+slug+"/settings", url.Values{
		"visibility":        {"public"},
		"member_visibility": {"member"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin saving settings: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	got, err := testServer.queries.GetGroup(t.Context(), groupID)
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if got.Visibility != db.GroupVisibilityKindConnections {
		t.Fatalf("admin should not be able to change visibility, got %s", got.Visibility)
	}

	// Owner can flip it.
	resp = formPost(t, alice, "/groups/"+slug+"/settings", url.Values{
		"visibility":        {"public"},
		"member_visibility": {"member"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner saving settings: status %d", resp.StatusCode)
	}
	resp.Body.Close()
	got, err = testServer.queries.GetGroup(t.Context(), groupID)
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if got.Visibility != db.GroupVisibilityKindPublic {
		t.Fatalf("owner-set visibility should be public, got %s", got.Visibility)
	}
}

func TestGroups_MemberVisibilityOwnerHidesListFromMembers(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	slug, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleMember, "private-members-group")

	if err := testServer.queries.UpdateGroupMemberVisibility(t.Context(), db.UpdateGroupMemberVisibilityParams{
		ID: groupID, MemberVisibility: db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("set member_visibility: %v", err)
	}

	// Bob (member) shouldn't see Alice's profile link via the member list —
	// his own nav avatar links to /users/bob regardless, so we check for the
	// other user's username plus the hidden-list explanation.
	body := readBody(t, get(t, bob, "/groups/"+slug))
	if strings.Contains(body, `href="/users/alice"`) {
		t.Fatalf("member should not see member list under owner-only visibility; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "member list is hidden") {
		t.Fatalf("expected hidden-list explanation; excerpt: %s", snippet(body))
	}

	// Alice (owner) still sees the full list and Bob's row in it.
	body = readBody(t, get(t, alice, "/groups/"+slug))
	if !strings.Contains(body, `href="/users/bob"`) {
		t.Fatalf("owner should see member list under owner-only visibility; excerpt: %s", snippet(body))
	}
}

// AddGroupMember by username on a user who is already an admin must not
// downgrade them to plain member. Before the ON CONFLICT change, the
// handler would silently flip a co-admin's role back to member every
// time another admin re-added them.
func TestGroups_AddMemberPreservesExistingRole(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	slug, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleAdmin, "no-downgrade-group")

	resp := formPost(t, alice, "/groups/"+slug+"/members", url.Values{
		"username": {"bob"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-add member: status %d", resp.StatusCode)
	}

	row, err := testServer.queries.GetGroupMembership(t.Context(), db.GetGroupMembershipParams{
		GroupID: groupID, UserID: bobUser.ID,
	})
	if err != nil {
		t.Fatalf("lookup membership: %v", err)
	}
	if row.Role != db.GroupMemberRoleAdmin {
		t.Fatalf("admin was downgraded by re-add; role=%s, want admin", row.Role)
	}
}

// Posting group settings without member_visibility used to silently
// default to the most-open option (member). Now the handler rejects the
// missing value and leaves the stored setting alone.
func TestGroups_SettingsRejectsMissingMemberVisibility(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bob := newClient(t)
	bobUser := signupAndGetUser(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	slug, groupID := seedGroupWith(t, alice, aliceUser, bobUser, db.GroupMemberRoleMember, "vis-required-group")

	// Owner sets member_visibility to owner (most restrictive).
	if err := testServer.queries.UpdateGroupMemberVisibility(t.Context(), db.UpdateGroupMemberVisibilityParams{
		ID: groupID, MemberVisibility: db.GroupMemberRoleOwner,
	}); err != nil {
		t.Fatalf("seed member visibility: %v", err)
	}

	// Partial form post — no member_visibility field. The handler should
	// re-render with a flash and leave the stored value alone.
	resp := formPost(t, alice, "/groups/"+slug+"/settings", url.Values{})
	body := readBody(t, resp)
	if !strings.Contains(body, "Pick a valid member-list visibility") {
		t.Fatalf("expected validation flash, got: %s", snippet(body))
	}

	g, err := testServer.queries.GetGroup(t.Context(), groupID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if g.MemberVisibility != db.GroupMemberRoleOwner {
		t.Fatalf("stored member_visibility changed despite missing form value; got %s, want owner", g.MemberVisibility)
	}
}
