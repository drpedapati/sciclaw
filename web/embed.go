package webui

import (
	"embed"
	"io/fs"
)

// distFS contains the built web UI bundle shipped with the binary.
//
//go:embed dist
var distFS embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
