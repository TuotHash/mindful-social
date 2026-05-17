package auth

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

const sessionUserKey = "user_id"

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
