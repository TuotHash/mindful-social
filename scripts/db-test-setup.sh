#!/usr/bin/env bash
# Create the mindful_social_test database and apply all migrations to it.
# Run once before `go test ./...` integration tests, and again whenever a
# new migration is added. Tests truncate the data tables between runs, so
# the schema persists across `go test` invocations.
set -euo pipefail

: "${PGHOST:?PGHOST must be set (run inside nix develop)}"
: "${PGPORT:?PGPORT must be set (run inside nix develop)}"
: "${TEST_DATABASE_URL:?TEST_DATABASE_URL must be set (run inside nix develop)}"

TEST_DB="mindful_social_test"

if psql -h "$PGHOST" -p "$PGPORT" -d postgres -tc \
    "SELECT 1 FROM pg_database WHERE datname = '$TEST_DB'" | grep -q 1; then
  echo "database '$TEST_DB' already exists"
else
  createdb -h "$PGHOST" -p "$PGPORT" "$TEST_DB"
  echo "created database '$TEST_DB'"
fi

cd "$(dirname "$0")/.."
goose -dir migrations postgres "$TEST_DATABASE_URL" up
