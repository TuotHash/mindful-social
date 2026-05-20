package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

const (
	sessionUserKey          = "user_id"
	sessionOIDCProviderKey  = "oidc_provider"
	sessionOIDCSubjectKey   = "oidc_subject"
	sessionOIDCSessionIDKey = "oidc_sid"
	sessionOIDCIDTokenKey   = "oidc_id_token"
)

// NewSessionManager wires scs to the Postgres-backed session store. We pass
// in a *sql.DB rather than the pgx pool because scs/postgresstore is built
// against database/sql; the caller bridges with stdlib.OpenDBFromPool.
// Sessions live in the `sessions` table created by migration 00001.
func NewSessionManager(db *sql.DB, secureCookie bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = postgresstore.NewWithCleanupInterval(db, 30*time.Minute)
	sm.Lifetime = 30 * 24 * time.Hour
	sm.IdleTimeout = 7 * 24 * time.Hour
	sm.Cookie.Name = "mindful_session"
	sm.Cookie.Persist = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secureCookie
	return sm
}

// LoginUser stores the user id in the current session. Always rotate the
// session token when privilege changes, to defeat session-fixation.
func LoginUser(ctx context.Context, sm *scs.SessionManager, userID uuid.UUID) error {
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, sessionUserKey, userID.String())
	return nil
}

// LogoutUser clears the session. Renewing the token also invalidates the old
// cookie server-side.
func LogoutUser(ctx context.Context, sm *scs.SessionManager) error {
	if err := sm.Destroy(ctx); err != nil {
		return err
	}
	return nil
}

// CurrentUserID extracts the logged-in user id from the session, if any.
func CurrentUserID(ctx context.Context, sm *scs.SessionManager) (uuid.UUID, bool) {
	raw := sm.GetString(ctx, sessionUserKey)
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// RecordOIDCLogin tags the current session with the IdP coordinates that
// produced it. We persist (provider, sub, sid) so a later backchannel
// logout from the IdP can find this exact device session and destroy it.
// sid may be empty when the IdP doesn't issue one — in that case backchannel
// logout falls back to revoking every session for (provider, sub).
// idToken is the raw signed id_token; we hold onto it so the user-initiated
// /logout flow can pass it back as `id_token_hint` to the IdP. May be empty
// for providers that never issued one (GitHub via plain OAuth2).
func RecordOIDCLogin(ctx context.Context, sm *scs.SessionManager, provider, subject, sid, idToken string) {
	sm.Put(ctx, sessionOIDCProviderKey, provider)
	sm.Put(ctx, sessionOIDCSubjectKey, subject)
	if sid != "" {
		sm.Put(ctx, sessionOIDCSessionIDKey, sid)
	}
	if idToken != "" {
		sm.Put(ctx, sessionOIDCIDTokenKey, idToken)
	}
}

// OIDCSessionLogout returns the IdP coordinates needed to start an
// RP-initiated logout. provider is the registry key (empty when the
// session wasn't established via OIDC); idToken is the raw id_token last
// issued for this session (empty when the provider never gave us one).
// Read before LogoutUser destroys the session — afterwards the values
// are gone from the session store.
func OIDCSessionLogout(ctx context.Context, sm *scs.SessionManager) (provider, idToken string) {
	return sm.GetString(ctx, sessionOIDCProviderKey), sm.GetString(ctx, sessionOIDCIDTokenKey)
}

// RevokeOIDCSessions destroys every session whose stored (provider, subject)
// matches. When sid is non-empty, only sessions also carrying that sid are
// destroyed — letting the IdP log a user out of one browser without
// disturbing their other devices. Returns the number of sessions destroyed.
func RevokeOIDCSessions(ctx context.Context, sm *scs.SessionManager, provider, subject, sid string) (int, error) {
	if provider == "" || subject == "" {
		return 0, errors.New("auth: revoke oidc: missing provider or subject")
	}
	var revoked int
	// Iterate runs against a fresh background context internally; pass one
	// rather than the request ctx so a short request deadline can't kill
	// the loop midway.
	err := sm.Iterate(context.Background(), func(c context.Context) error {
		if sm.GetString(c, sessionOIDCProviderKey) != provider {
			return nil
		}
		if sm.GetString(c, sessionOIDCSubjectKey) != subject {
			return nil
		}
		if sid != "" && sm.GetString(c, sessionOIDCSessionIDKey) != sid {
			return nil
		}
		if err := sm.Destroy(c); err != nil {
			return err
		}
		revoked++
		return nil
	})
	return revoked, err
}

// RevokeUserSessions destroys every active session belonging to userID.
// When preserveCurrent is true, the session attached to ctx is left
// alone so the caller stays logged in — useful for self-service
// password/email changes. Admin-triggered changes pass false to log the
// target out of every device.
func RevokeUserSessions(ctx context.Context, sm *scs.SessionManager, userID uuid.UUID, preserveCurrent bool) error {
	keep := ""
	if preserveCurrent {
		keep = sm.Token(ctx)
	}
	target := userID.String()
	// Iterate runs against a fresh background context internally so we
	// pass one rather than the request ctx, which may carry a deadline
	// short enough to kill the loop midway.
	return sm.Iterate(context.Background(), func(c context.Context) error {
		if keep != "" && sm.Token(c) == keep {
			return nil
		}
		if sm.GetString(c, sessionUserKey) != target {
			return nil
		}
		return sm.Destroy(c)
	})
}
