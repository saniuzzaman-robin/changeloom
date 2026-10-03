// Package seed embeds the curator's topic catalog.
package seed

import (
	"embed"
	"io/fs"
)

//go:embed catalog
var files embed.FS

// Catalog is the catalog directory loaded by `curator seed`: professions.yaml and one
// topics/<root>.yaml per root topic.
var Catalog = func() fs.FS {
	sub, err := fs.Sub(files, "catalog")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}()
