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
func NewSessionManager(db *sql.DB) *scs.SessionManager {
	sm := scs.New()
	sm.Store = postgresstore.NewWithCleanupInterval(db, 30*time.Minute)
	sm.Lifetime = 30 * 24 * time.Hour
	sm.IdleTimeout = 7 * 24 * time.Hour
	sm.Cookie.Name = "mindful_session"
	sm.Cookie.Persist = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.HttpOnly = true
	// Cookie.Secure stays false for local HTTP dev; the reverse proxy
	// terminates TLS in production and the browser treats the connection
	// as secure regardless. If you want belt-and-braces, set Secure=true
	// once you only ever serve through HTTPS.
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
