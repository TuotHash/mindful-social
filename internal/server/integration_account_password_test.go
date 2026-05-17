package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Changing the password from one device should sign out every other
// session belonging to the same user, while keeping the current session
// alive so the user isn't bounced to /login mid-flow.
func TestAccountPasswordChange_RevokesOtherSessions(t *testing.T) {
	integrationDB(t)
	const email = "alice@example.com"
	const password = "correct horse battery staple"
	const newPassword = "fresh saddle hilltop banjo"

	primary := newClient(t)
	signup(t, primary, "alice", email, password)

	// Open a second session as Alice — simulates a logged-in device an
	// attacker might be sitting on.
	second := newClient(t)
	resp := formPost(t, second, "/login", url.Values{
		"email":    {email},
		"password": {password},
	})
	resp.Body.Close()
	if strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Fatalf("second login did not succeed; URL=%s", resp.Request.URL.Path)
	}
	// Sanity: the second session can see the home page as alice.
	if !strings.Contains(readBody(t, get(t, second, "/")), ">alice<") {
		t.Fatal("second session not logged in before password change")
	}

	// Primary changes the password.
	resp = formPost(t, primary, "/account/password", url.Values{
		"current_password": {password},
		"new_password":     {newPassword},
		"confirm_password": {newPassword},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password change: status %d", resp.StatusCode)
	}

	// Primary stays logged in.
	if !strings.Contains(readBody(t, get(t, primary, "/")), ">alice<") {
		t.Fatal("primary session was logged out by its own password change")
	}

	// Second session is revoked.
	if strings.Contains(readBody(t, get(t, second, "/")), ">alice<") {
		t.Fatal("second session was NOT revoked by primary's password change")
	}
}
