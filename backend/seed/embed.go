// Package seed embeds the reference data loaded into the database.
package seed

import _ "embed"

// TopicsYAML is the topic tree synced into the topics table.
//
//go:embed topics.yaml
var TopicsYAML []byte

// SourcesYAML is the source list synced into the sources table.
//
//go:embed sources.yaml
var SourcesYAML []byte
