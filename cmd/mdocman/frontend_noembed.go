//go:build noembed

package main

import "net/http"

func hasEmbeddedFrontend() bool {
	return false
}

// Split builds leave frontend delivery to the desktop shell or a separate host.
func embeddedFrontendHandler() http.Handler {
	return http.NotFoundHandler()
}
