// Package migrations embeds the curator-only goose migrations. They run after the backend
// migrations and are tracked in their own goose table.
package migrations

import "embed"

// FS holds the goose migration files.
//
//go:embed *.sql
var FS embed.FS
