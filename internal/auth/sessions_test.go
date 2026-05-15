package auth

import "testing"

func TestNewSessionManagerSecureCookie(t *testing.T) {
	t.Parallel()

	sm := NewSessionManager(nil, true)

	if !sm.Cookie.Secure {
		t.Fatal("expected secure session cookie")
	}
}

func TestNewSessionManagerInsecureCookie(t *testing.T) {
	t.Parallel()

	sm := NewSessionManager(nil, false)

	if sm.Cookie.Secure {
		t.Fatal("expected insecure session cookie")
	}
}
