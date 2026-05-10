package auth

import (
	"context"
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
		if err := seedTrustedList(ctx, q, u.ID); err != nil {
			return err
		}
		user = u
		return nil
	})
	return user, err
}

// seedTrustedList creates the per-user built-in "Trusted" list. Called
// inside the signup transaction so a user always exits signup with their
// trusted list available to the visibility selector.
func seedTrustedList(ctx context.Context, q *db.Queries, userID uuid.UUID) error {
	_, err := q.CreateAudienceList(ctx, db.CreateAudienceListParams{
		OwnerID:   userID,
		Name:      "Trusted",
		IsTrusted: true,
	})
	return err
}

// AuthenticatePassword returns the user id on a successful email+password
// match, ErrInvalidLogin otherwise. The error message is intentionally
// vague to avoid leaking whether the email exists.
func (s *Service) AuthenticatePassword(ctx context.Context, email, password string) (uuid.UUID, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	row, err := s.q.GetPasswordIdentityForLogin(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrInvalidLogin
		}
		return uuid.Nil, err
	}
	if row.PasswordHash == nil {
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

	// No identity yet. Try to attach to an existing user by email.
	if ident.Email != "" {
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
	username, email := suggestIdentity(ident)
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
		if err := seedTrustedList(ctx, q, u.ID); err != nil {
			return err
		}
		user = u
		return nil
	})
	return user, err
}

// suggestIdentity picks a starting username + email for an OAuth signup.
// Username may collide; uniqueUsername resolves that downstream.
func suggestIdentity(ident Identity) (username, email string) {
	email = strings.ToLower(strings.TrimSpace(ident.Email))
	switch {
	case ident.DisplayName != "":
		username = sanitizeUsername(ident.DisplayName)
	case email != "":
		username = sanitizeUsername(strings.SplitN(email, "@", 2)[0])
	default:
		username = "user"
	}
	if username == "" {
		username = "user"
	}
	if email == "" {
		// Fallback for providers (e.g., GitHub with hidden email): use a
		// placeholder; user can change it later. UNIQUE on users.email
		// means we still need a real-looking value.
		email = ident.Subject + "@no-email.local"
	}
	return username, email
}

var sanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeUsername(s string) string {
	s = strings.TrimSpace(s)
	s = sanitizeRE.ReplaceAllString(s, "_")
	if len(s) > 32 {
		s = s[:32]
	}
	if len(s) < 3 {
		s = "user_" + s
	}
	return s
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
