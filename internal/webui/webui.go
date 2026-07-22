// Package webui embeds the built Svelte SPA so `gbg serve` ships as a single
// binary. The dist/ directory is populated by `make web` (vite build + copy)
// before `go build`; when it holds only the placeholder, FS reports absent and
// serve falls back to --web-dir or API-only.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded SPA filesystem rooted at dist/, and whether a real
// build is present (index.html exists).
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false // only the placeholder is embedded
	}
	return sub, true
}
