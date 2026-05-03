#!/usr/bin/env bash
# Roll back the most recent migration.
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL must be set (run inside nix develop)}"

cd "$(dirname "$0")/.."
goose -dir migrations postgres "$DATABASE_URL" down
