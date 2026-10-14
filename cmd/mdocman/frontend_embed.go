//go:build !noembed

package main

import (
	"embed"
	"io/fs"
	"net/http"
)

// build.sh cli temporarily copies the prerendered Vinext application into this
// directory before compiling. The tracked placeholder lets ordinary go test
// and go run commands work without a production frontend build.
//
//go:embed all:frontend_dist
var embeddedFrontend embed.FS

func hasEmbeddedFrontend() bool {
	info, err := fs.Stat(embeddedFrontend, "frontend_dist/index.html")
	return err == nil && !info.IsDir()
}

func embeddedFrontendHandler() http.Handler {
	files, err := fs.Sub(embeddedFrontend, "frontend_dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return newFrontendHandler(files)
}
