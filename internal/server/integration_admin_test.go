package server

import (
	"net/url"
	"testing"

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
