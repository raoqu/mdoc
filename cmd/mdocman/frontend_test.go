package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestFrontendHandlerServesIndexAssetsAndSPAFallback(t *testing.T) {
	handler := newFrontendHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Mdoc</title>")},
		"assets/app.js": {Data: []byte("console.log('mdoc')")},
	})

	for _, requestPath := range []string{"/", "/settings"} {
		response := requestFrontend(t, handler, requestPath)
		if response.Code != http.StatusOK || response.Body.String() != "<!doctype html><title>Mdoc</title>" {
			t.Fatalf("GET %s = %d %q", requestPath, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("GET %s cache control = %q", requestPath, got)
		}
	}

	asset := requestFrontend(t, handler, "/assets/app.js")
	if asset.Code != http.StatusOK || asset.Body.String() != "console.log('mdoc')" {
		t.Fatalf("asset = %d %q", asset.Code, asset.Body.String())
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache control = %q", got)
	}
}

func TestFrontendHandlerDoesNotMaskBackendOrMissingFiles(t *testing.T) {
	handler := newFrontendHandler(fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html>")},
	})
	for _, requestPath := range []string{"/api", "/api/missing", "/_mdoc/themes/default.css", "/uploads/missing.png", "/missing.js", "/.gitignore", "/.vite/manifest.json"} {
		response := requestFrontend(t, handler, requestPath)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", requestPath, response.Code)
		}
	}
}

func TestEmbeddedFrontendWithoutBuildReturnsNotFound(t *testing.T) {
	response := requestFrontend(t, embeddedFrontendHandler(), "/")
	if response.Code != http.StatusNotFound {
		t.Fatalf("embedded development placeholder = %d, want 404", response.Code)
	}
}

func requestFrontend(t *testing.T, handler http.Handler, requestPath string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
