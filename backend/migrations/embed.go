// Package migrations embeds the goose SQL migrations so tests and binaries can apply them.
package migrations

import "embed"

// FS holds the goose migration files.
//
//go:embed *.sql
var FS embed.FS
