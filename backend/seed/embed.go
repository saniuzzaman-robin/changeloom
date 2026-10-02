// Package seed embeds the reference data loaded into the database.
package seed

import _ "embed"

// TopicsYAML is a test/dev topic tree; the curator owns the real topic catalog.
//
//go:embed topics.yaml
var TopicsYAML []byte
