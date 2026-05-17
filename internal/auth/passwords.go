package auth

import (
	"errors"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is intentionally a single source of truth so the form,
// the API, and the docs can't drift apart.
const MinPasswordLength = 12

var (
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 1024 characters")
)

// HashPassword wraps bcrypt with our policy checks. The 1024-char ceiling is a
// belt-and-braces guard against bcrypt's 72-byte truncation surprising callers
// who paste in long passphrases.
func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(password) > 1024 {
		return "", ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword returns nil on a match, or an error otherwise.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// EqualizePasswordTiming runs a bcrypt comparison against a fixed dummy
// hash so the unknown-email login path takes the same wall-clock time as
// the wrong-password path. Without this, a single failed login leaks
// "email exists" via response time (~1 ms vs ~80 ms at default cost).
// The hash is computed once on first use and reused thereafter.
func EqualizePasswordTiming() {
	dummyHashOnce.Do(func() {
		// The password text doesn't matter — CompareHashAndPassword runs
		// the same way for matching and non-matching inputs.
		h, err := bcrypt.GenerateFromPassword([]byte("equalize-login-timing"), bcrypt.DefaultCost)
		if err == nil {
			dummyHash = h
		}
	})
	if dummyHash != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("equalize-login-timing"))
	}
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)
