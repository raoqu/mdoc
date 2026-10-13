package hugoengine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	source := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func TestBuildNativeThemeWithoutExternalPrograms(t *testing.T) {
	source := fixture(t, map[string]string{
		"hugo.toml":                                   "title = 'Embedded site'\ntheme = 'fixture'\n",
		"themes/fixture/layouts/index.html":           `<html><head><title>{{ .Site.Title }}</title><link rel="stylesheet" href="{{ "theme.css" | relURL }}"></head><body>{{ range .Site.RegularPages }}<a href="{{ .RelPermalink }}">{{ .Title }}</a>{{ end }}</body></html>`,
		"themes/fixture/layouts/_default/single.html": `<article>{{ .Content }}</article>`,
		"themes/fixture/static/theme.css":             "body { color: #123456; }",
		"content/post/note/index.md":                  "---\ntitle: Native note\n---\n# Compiled inside Mdoc\n\n**Rendered Markdown**\n",
	})
	destination := filepath.Join(t.TempDir(), "public")
	t.Setenv("PATH", t.TempDir())
	// Host environment must not override the caller's source, base URL or output.
	t.Setenv("HUGO_BASEURL", "https://wrong.example/")
	t.Setenv("HUGO_PUBLISHDIR", filepath.Join(t.TempDir(), "wrong"))
	log, err := Build(context.Background(), source, destination, "https://example.test/docs/")
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		"index.html":           "/docs/post/note/",
		"post/note/index.html": "<strong>Rendered Markdown</strong>",
		"theme.css":            "#123456",
	} {
		data, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), expected) {
			t.Fatalf("%s missing %s: %s", name, expected, data)
		}
	}
	if !strings.Contains(log, "Mdoc 内置 Hugo") {
		t.Fatal("missing embedded engine build information")
	}
}

func TestBuildCompilesSassInProcess(t *testing.T) {
	if !Extended() {
		t.Skip("Sass is included by go test -tags extended")
	}
	source := fixture(t, map[string]string{
		"hugo.toml":          "title = 'Sass site'\n",
		"layouts/index.html": `{{ $css := resources.Get "main.scss" | toCSS }}<link rel="stylesheet" href="{{ $css.RelPermalink }}">`,
		"assets/main.scss":   "$color: #123456; .article { .title { color: $color; } }",
	})
	destination := filepath.Join(t.TempDir(), "public")
	t.Setenv("PATH", t.TempDir())
	if _, err := Build(context.Background(), source, destination, "https://example.test/"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "main.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".article .title") || !strings.Contains(string(data), "#123456") {
		t.Fatalf("Sass not compiled: %s", data)
	}
}

func TestBuildReturnsTemplateErrorsAndCancellation(t *testing.T) {
	source := fixture(t, map[string]string{"hugo.toml": "title = 'Broken site'\n", "layouts/index.html": "{{ undefinedFunction }}"})
	t.Setenv("PATH", t.TempDir())
	if _, err := Build(context.Background(), source, t.TempDir(), "https://example.test/"); err == nil {
		t.Fatal("invalid template accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, source, t.TempDir(), "https://example.test/"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := VendorTheme(ctx, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("installation cancellation: %v", err)
	}
}
