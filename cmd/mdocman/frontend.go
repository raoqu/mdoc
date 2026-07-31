package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// build.sh temporarily copies the prerendered Vinext application into this
// directory before compiling. Keeping the directory itself in git also lets
// ordinary `go test` and `go run` commands work without a production build.
//
//go:embed all:frontend_dist
var embeddedFrontend embed.FS

type frontendHandler struct {
	files fs.FS
}

func embeddedFrontendHandler() http.Handler {
	files, err := fs.Sub(embeddedFrontend, "frontend_dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return newFrontendHandler(files)
}

func newFrontendHandler(files fs.FS) http.Handler {
	return &frontendHandler{files: files}
}

func (h *frontendHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if isBackendPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		h.serveIndex(w, r)
		return
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			http.NotFound(w, r)
			return
		}
	}
	if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServer(http.FS(h.files)).ServeHTTP(w, r)
		return
	}
	if path.Ext(name) == "" {
		h.serveIndex(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *frontendHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	index, err := fs.ReadFile(h.files, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(index)
}

func isBackendPath(requestPath string) bool {
	for _, prefix := range []string{"/api", "/_mdoc", "/uploads", "/audio", "/site", "/s"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}
