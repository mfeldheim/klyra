// internal/server/embed.go
package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedUI embed.FS

// UIFileSystem returns the embedded UI dist as an fs.FS rooted at "dist".
func UIFileSystem() fs.FS {
	sub, err := fs.Sub(embeddedUI, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
