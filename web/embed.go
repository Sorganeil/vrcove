// Package web holds the built single-page frontend. `npm run build` writes it
// to web/dist before `go build`, which embeds it into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend rooted at its output directory.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
