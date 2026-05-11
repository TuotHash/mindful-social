// Package migrate runs goose migrations from the embedded MigrationsFS.
// Called once during server startup so a fresh `mindful-social` binary
// brings its own schema with it — no external goose CLI required on the
// deploy host.
package migrate

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	mindfulsocial "github.com/TuotHash/mindful-social"
)

// Up applies all pending migrations against db. Safe to call on every
// boot: goose tracks applied versions in goose_db_version and only runs
// what's missing.
func Up(db *sql.DB) error {
	goose.SetBaseFS(mindfulsocial.MigrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
