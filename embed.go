// Package mindfulsocial holds the embedded assets (static files, SQL
// migrations) and project-wide constants. Living at the module root lets
// `go:embed` reach static/ and migrations/ without moving them out of the
// places dev tooling expects.
package mindfulsocial

import "embed"

// Version is the human-readable release tag. Bumped per release.
const Version = "1.0.0-alpha"

// StaticFS holds the contents of ./static (CSS, vendored htmx, etc.),
// served at /static/* by the HTTP server. Using all: includes dotfiles
// like .gitkeep should they appear; harmless either way.
//
//go:embed all:static
var StaticFS embed.FS

// MigrationsFS holds the goose .sql files under ./migrations. The
// embedded form is used by the runtime migration runner so the binary
// is self-contained — no external goose CLI or on-disk migration
// directory required at deploy time.
//
//go:embed migrations
var MigrationsFS embed.FS
