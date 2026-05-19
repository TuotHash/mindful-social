package server

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// Two admins demoting one of each other is fine — count goes from 2 to 1,
// the guard checks "count <= 1 *before* the demotion", so it stays out of
// the way. After the demotion CountAdmins reports the new total so the
// next attempt can fire the guard.
func TestAdminSetRole_DemotionUpdatesAdminCount(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	aliceUser := signupAndGetUser(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	bobUser := signupAndGetUser(t, newClient(t), "bob", "bob@example.com", "correct horse battery staple")

	for _, u := range []db.User{aliceUser, bobUser} {
		if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
			ID: u.ID, Role: db.UserRoleAdmin,
		}); err != nil {
			t.Fatalf("seed admin %s: %v", u.Username, err)
		}
	}

	// Alice demotes bob — allowed because count is 2.
	resp := formPost(t, alice, "/admin/users/"+bobUser.ID.String()+"/role", url.Values{
		"role": {"user"},
	})
	resp.Body.Close()
	row, err := testServer.queries.GetUser(t.Context(), bobUser.ID)
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	if row.Role != db.UserRoleUser {
		t.Fatalf("bob role = %s, want user", row.Role)
	}

	// CountAdmins now reflects the demotion. The next attempt to demote
	// an admin while count <= 1 would hit the guard — exercising that
	// path end-to-end requires fabricating a race because the
	// self-demote check fires first when only one admin remains and
	// they're the actor.
	count, err := testServer.queries.CountAdmins(t.Context())
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountAdmins = %d, want 1", count)
	}
}

// Admins can delete other users. The delete cascades to nodes the target
// authored — that's the schema change in migration 00029 — so the test
// seeds a node first to exercise the constraint that used to RESTRICT.
func TestAdminDeleteUser_RemovesUserAndCascadesNodes(t *testing.T) {
	integrationDB(t)
	adminClient := newClient(t)
	adminUser := signupAndGetUser(t, adminClient, "admin1", "admin1@example.com", "correct horse battery staple")
	targetClient := newClient(t)
	targetUser := signupAndGetUser(t, targetClient, "target", "target@example.com", "correct horse battery staple")

	if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
		ID: adminUser.ID, Role: db.UserRoleAdmin,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	// Target authors a node so the delete has to cascade through nodes
	// (which would have failed under the pre-00029 RESTRICT).
	nodeID := createNode(t, targetClient, "topic", "Target's topic", "")

	resp := formPost(t, adminClient, "/admin/users/"+targetUser.ID.String()+"/delete", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete user: status %d", resp.StatusCode)
	}

	if _, err := testServer.queries.GetUser(t.Context(), targetUser.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected user deleted, got err=%v", err)
	}
	if _, err := testServer.queries.GetNode(t.Context(), nodeID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected authored node cascaded, got err=%v", err)
	}
}

// Self-delete is refused even when the URL is hit directly. The handler
// flashes an error and redirects back to /admin without touching the
// user row — there's no path for an admin to delete themselves through
// the UI.
func TestAdminDeleteUser_RefusesSelfDelete(t *testing.T) {
	integrationDB(t)
	adminClient := newClient(t)
	adminUser := signupAndGetUser(t, adminClient, "soloadmin", "solo@example.com", "correct horse battery staple")
	if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
		ID: adminUser.ID, Role: db.UserRoleAdmin,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	resp := formPost(t, adminClient, "/admin/users/"+adminUser.ID.String()+"/delete", nil)
	resp.Body.Close()

	if _, err := testServer.queries.GetUser(t.Context(), adminUser.ID); err != nil {
		t.Fatalf("self-delete should have been refused, but user is gone: %v", err)
	}
}

// A non-admin target deletes cleanly even when the actor is the only
// admin — the last-admin guard doesn't fire because the target's role
// is irrelevant to admin-count preservation.
func TestAdminDeleteUser_NonAdminTargetWithSoloAdmin(t *testing.T) {
	integrationDB(t)
	adminClient := newClient(t)
	adminUser := signupAndGetUser(t, adminClient, "solo2", "solo2@example.com", "correct horse battery staple")
	targetUser := signupAndGetUser(t, newClient(t), "regular", "regular@example.com", "correct horse battery staple")
	if err := testServer.queries.UpdateUserRole(t.Context(), db.UpdateUserRoleParams{
		ID: adminUser.ID, Role: db.UserRoleAdmin,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	resp := formPost(t, adminClient, "/admin/users/"+targetUser.ID.String()+"/delete", nil)
	resp.Body.Close()

	if _, err := testServer.queries.GetUser(t.Context(), targetUser.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected regular user deleted, got err=%v", err)
	}
}
