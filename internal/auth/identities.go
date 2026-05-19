package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TuotHash/mindful-social/internal/db"
)

var (
	ErrUserExists       = errors.New("user with this email or username already exists")
	ErrInvalidEmail     = errors.New("invalid email")
	ErrInvalidUsername  = errors.New("username must be 3-32 chars, letters/digits/_/-/.")
	ErrInvalidLogin     = errors.New("invalid email or password")
	ErrLinkConflict     = errors.New("this provider account is linked to a different user")
	ErrUsernameTaken    = errors.New("username is taken")
	ErrLastIdentity     = errors.New("can't remove your last sign-in method")
	ErrIdentityNotFound = errors.New("sign-in method not found")
	ErrNoPassword       = errors.New("no password set for this account")
	ErrPasswordExists   = errors.New("password is already set; use change instead")
	usernameValidRegex  = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)
	emailValidRegex     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// Service bundles the user/identity write paths so handlers don't have to
// juggle the pool, queries, and validation rules separately.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool)}
}

// SignupWithPassword creates a user and a 'password' identity in one tx.
func (s *Service) SignupWithPassword(ctx context.Context, username, email, password string) (db.User, error) {
	username = strings.TrimSpace(username)
	email = strings.ToLower(strings.TrimSpace(email))

	if !usernameValidRegex.MatchString(username) {
		return db.User{}, ErrInvalidUsername
	}
	if !emailValidRegex.MatchString(email) {
		return db.User{}, ErrInvalidEmail
	}
	hash, err := HashPassword(password)
	if err != nil {
		return db.User{}, err
	}

	var user db.User
	err = s.tx(ctx, func(q *db.Queries) error {
		u, err := q.CreateUser(ctx, db.CreateUserParams{Username: username, Email: email})
		if err != nil {
			return mapUserConflict(err)
		}
		_, err = q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:   u.ID,
			Provider: "password",
			Subject:  email,
			Secret:   &hash,
		})
		if err != nil {
			return err
		}
		user = u
		return nil
	})
	return user, err
}

// AuthenticatePassword returns the user id on a successful email+password
// match, ErrInvalidLogin otherwise. The error message is intentionally
// vague to avoid leaking whether the email exists, and the unknown-email
// branch runs a dummy bcrypt comparison so its wall-clock time matches
// the wrong-password branch (otherwise ~1 ms vs ~80 ms reveals the
// difference).
func (s *Service) AuthenticatePassword(ctx context.Context, email, password string) (uuid.UUID, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	row, err := s.q.GetPasswordIdentityForLogin(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			EqualizePasswordTiming()
			return uuid.Nil, ErrInvalidLogin
		}
		return uuid.Nil, err
	}
	if row.PasswordHash == nil {
		EqualizePasswordTiming()
		return uuid.Nil, ErrInvalidLogin
	}
	if err := CheckPassword(*row.PasswordHash, password); err != nil {
		return uuid.Nil, ErrInvalidLogin
	}
	return row.ID, nil
}

// FindOrCreateOAuthUser handles the post-callback bookkeeping:
//   - if the (provider, subject) pair exists, return that user;
//   - otherwise, if a user with that email exists, link the new identity to
//     them (account linking);
//   - otherwise, create a fresh user (auto-username derived from email/name)
//     plus a fresh identity.
func (s *Service) FindOrCreateOAuthUser(ctx context.Context, provider string, ident Identity) (db.User, error) {
	if ident.Subject == "" {
		return db.User{}, errors.New("oauth: provider returned empty subject")
	}

	// Existing identity → existing user.
	if found, err := s.q.GetIdentityByProvider(ctx, db.GetIdentityByProviderParams{
		Provider: provider, Subject: ident.Subject,
	}); err == nil {
		return s.q.GetUser(ctx, found.UserID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, err
	}

	// No identity yet. Try to attach to an existing user by email — but only
	// when the IdP attests the email is verified. Otherwise an attacker who
	// controls a tenant on a generic OIDC issuer could pick any victim's
	// email, complete the IdP flow, and land in the victim's account.
	if ident.Email != "" && ident.EmailVerified {
		if existing, err := s.q.GetUserByEmail(ctx, strings.ToLower(ident.Email)); err == nil {
			_, err := s.q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
				UserID:   existing.ID,
				Provider: provider,
				Subject:  ident.Subject,
				Secret:   nil,
			})
			if err != nil {
				return db.User{}, err
			}
			return existing, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, err
		}
	}

	// Fresh user.
	username, email := suggestIdentity(provider, ident)
	var user db.User
	err := s.tx(ctx, func(q *db.Queries) error {
		uname, err := s.uniqueUsername(ctx, q, username)
		if err != nil {
			return err
		}
		u, err := q.CreateUser(ctx, db.CreateUserParams{Username: uname, Email: email})
		if err != nil {
			return mapUserConflict(err)
		}
		_, err = q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:   u.ID,
			Provider: provider,
			Subject:  ident.Subject,
			Secret:   nil,
		})
		if err != nil {
			return err
		}
		user = u
		return nil
	})
	return user, err
}

// suggestIdentity picks a starting username + email for an OAuth signup.
//
// Username preference order:
//  1. The IdP's display-name claim (OIDC preferred_username / name,
//     GitHub login), sanitised into the local handle alphabet.
//  2. A deterministic u_<sha256(provider:subject)[:8]> placeholder when
//     the claim is absent or unusable.
//
// Trading off: using preferred_username spares the user a rename, but
// on a multi-tenant OIDC issuer an attacker could set their own
// preferred_username to "alice" and grab that handle before the real
// Alice signs up. We accept that risk for the self-hosted / trusted-IdP
// case; uniqueUsername still appends a numeric suffix on collisions so
// the worst case is "alice2", not a hijack of an existing account
// (identity linking still requires email_verified).
//
// The email is only carried over when the IdP attested verification.
// Otherwise we use a per-subject placeholder so an attacker can't block
// real signups by claiming a victim's address at the IdP.
func suggestIdentity(provider string, ident Identity) (username, email string) {
	h := sha256.Sum256([]byte(provider + ":" + ident.Subject))
	short := hex.EncodeToString(h[:4]) // 8 hex chars
	username = cleanUsernameSeed(ident.DisplayName)
	if username == "" {
		username = "u_" + short
	}
	if ident.EmailVerified {
		email = strings.ToLower(strings.TrimSpace(ident.Email))
	}
	if email == "" {
		// UNIQUE on users.email means we still need a real-looking value.
		// Deterministic so a re-attempted signup with the same (provider,
		// subject) doesn't burn through a fresh address each time.
		email = "u_" + short + "@no-email.local"
	}
	return username, email
}

// cleanUsernameSeed coerces an IdP-supplied handle into the local
// username alphabet. Characters outside [a-zA-Z0-9._-] are dropped
// (so "John Doe" → "JohnDoe", "alice@example.com" → "aliceexample.com"),
// leading/trailing punctuation is trimmed, and the result is returned
// only when it matches usernameValidRegex. Anything else yields "" so
// the caller falls back to the hash placeholder.
func cleanUsernameSeed(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	cleaned := strings.Trim(b.String(), "._-")
	if len(cleaned) > 32 {
		cleaned = strings.TrimRight(cleaned[:32], "._-")
	}
	if !usernameValidRegex.MatchString(cleaned) {
		return ""
	}
	return cleaned
}

// uniqueUsername returns base, base2, base3, … until it finds one that's
// free. Bounded so we can't spin forever on a hostile collision.
func (s *Service) uniqueUsername(ctx context.Context, q *db.Queries, base string) (string, error) {
	candidate := base
	for i := 2; i < 100; i++ {
		_, err := q.GetUserByUsername(ctx, candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s%d", base, i)
		if len(candidate) > 32 {
			candidate = candidate[:32]
		}
	}
	return "", ErrUsernameTaken
}

// ChangePassword verifies the user's current password and replaces the
// bcrypt hash on their existing password identity. Returns ErrInvalidLogin
// when the current password doesn't match so callers can show a generic
// error without leaking which step failed.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string) error {
	ident, err := s.q.GetPasswordIdentityForUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPassword
		}
		return err
	}
	if ident.Secret == nil {
		return ErrInvalidLogin
	}
	if err := CheckPassword(*ident.Secret, current); err != nil {
		return ErrInvalidLogin
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.q.UpdatePasswordIdentitySecret(ctx, db.UpdatePasswordIdentitySecretParams{
		UserID: userID,
		Secret: &hash,
	})
}

// SetInitialPassword adds a password identity for a user who currently
// has none — i.e. signed up via OAuth and now wants email-and-password as
// a backup sign-in. The new identity's subject is the user's email so
// the existing login lookup keeps working.
func (s *Service) SetInitialPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if _, err := s.q.GetPasswordIdentityForUser(ctx, userID); err == nil {
		return ErrPasswordExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	user, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = s.q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
		UserID:   userID,
		Provider: "password",
		Subject:  strings.ToLower(strings.TrimSpace(user.Email)),
		Secret:   &hash,
	})
	return err
}

// AdminUpdateUsername changes a user's username with the same validation as
// signup. No self-check or current-password — admins are trusted; the audit
// trail comes from the surrounding handler's logs.
func (s *Service) AdminUpdateUsername(ctx context.Context, userID uuid.UUID, username string) error {
	username = strings.TrimSpace(username)
	if !usernameValidRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	if err := s.q.UpdateUserUsername(ctx, db.UpdateUserUsernameParams{
		ID:       userID,
		Username: username,
	}); err != nil {
		return mapUserConflict(err)
	}
	return nil
}

// AdminUpdateEmail changes a user's email with the same validation as signup
// and keeps the password identity's subject in sync so it never drifts from
// the user's current email. The login lookup uses users.email, so a stale
// subject wouldn't break sign-in — but it's a footgun for anything else that
// might join on (provider, subject) in the future.
func (s *Service) AdminUpdateEmail(ctx context.Context, userID uuid.UUID, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailValidRegex.MatchString(email) {
		return ErrInvalidEmail
	}
	return s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateUserEmail(ctx, db.UpdateUserEmailParams{
			ID:    userID,
			Email: email,
		}); err != nil {
			return mapUserConflict(err)
		}
		// Best-effort subject sync. The query is a no-op when the user has
		// no password identity, so there's nothing to branch on.
		return q.UpdatePasswordIdentitySubject(ctx, db.UpdatePasswordIdentitySubjectParams{
			UserID:  userID,
			Subject: email,
		})
	})
}

// AdminResetPassword sets a user's password without knowing the previous one.
// Creates a password identity when the user is OAuth-only — i.e. the admin
// effectively "issues" a password so the user can sign in by email as a
// fallback (and then change it themselves on /account).
func (s *Service) AdminResetPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = s.q.GetPasswordIdentityForUser(ctx, userID)
	switch {
	case err == nil:
		return s.q.UpdatePasswordIdentitySecret(ctx, db.UpdatePasswordIdentitySecretParams{
			UserID: userID,
			Secret: &hash,
		})
	case errors.Is(err, pgx.ErrNoRows):
		user, err := s.q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		_, err = s.q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:   userID,
			Provider: "password",
			Subject:  strings.ToLower(strings.TrimSpace(user.Email)),
			Secret:   &hash,
		})
		return err
	default:
		return err
	}
}

// DisconnectIdentity removes a sign-in method from the user's account.
// Refuses to remove the last remaining identity so the user can never
// lock themselves out.
func (s *Service) DisconnectIdentity(ctx context.Context, userID, identityID uuid.UUID) error {
	if _, err := s.q.GetIdentityForUser(ctx, db.GetIdentityForUserParams{
		ID: identityID, UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIdentityNotFound
		}
		return err
	}
	n, err := s.q.CountIdentitiesForUser(ctx, userID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastIdentity
	}
	return s.q.DeleteAuthIdentityForUser(ctx, db.DeleteAuthIdentityForUserParams{
		ID: identityID, UserID: userID,
	})
}

// tx runs fn inside a transaction, committing on success and rolling back on
// any returned error. Keeping multi-statement signup atomic guards against
// orphan users with no identity (or vice versa).
func (s *Service) tx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// mapUserConflict turns a unique-violation on users.email or users.username
// into our public error. Anything else passes through unchanged.
func mapUserConflict(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "users_email_key", "users_username_key":
			return ErrUserExists
		}
	}
	return err
}
