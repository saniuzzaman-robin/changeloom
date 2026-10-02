// Package seed embeds the curator's topic catalog.
package seed

import _ "embed"

// CatalogYAML is the topic tree with relations and source hints, loaded by `curator seed`.
//
//go:embed catalog.yaml
var CatalogYAML []byte
