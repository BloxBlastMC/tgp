// Package web embeds the built web UI (web/dist, produced by `pnpm build`)
// into the tgp-server binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built UI file tree rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is always present (dist/.gitkeep is committed)
	}
	return sub
}
