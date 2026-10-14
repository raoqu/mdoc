package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	themeassets "mdocman/themes"
)

func TestThemeDiscoveryInheritanceAndLiveTemplates(t *testing.T) {
	directory := t.TempDir()
	for _, id := range []string{"_shared", "default", "stack"} {
		if err := copyThemeComponent(themeassets.Files, id, filepath.Join(directory, id)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MDOC_THEME_DIR", directory)
	writeTestFile(t, filepath.Join(directory, "ocean", "theme.json"), `{"id":"ocean","label":"Ocean","extends":"default","treeTheme":"compact"}`)
	writeTestFile(t, filepath.Join(directory, "ocean", "layouts", "_default", "single.html"), `{{ define "main" }}<h1>OCEAN_LAYOUT</h1>{{ .Content }}{{ end }}`)
	writeTestFile(t, filepath.Join(directory, "ocean", "static", "mdoc", "style.css"), `:root { --preview-link: #123456; }`)
	themes, err := hugoThemes()
	if err != nil {
		t.Fatal(err)
	}
	if len(themes) != 3 || themes[0].ID != "default" {
		t.Fatalf("themes not discovered: %+v", themes)
	}
	s := &server{dataDir: t.TempDir()}
	render := func() string {
		t.Helper()
		html, err := s.renderHugoPreview(context.Background(), publicationConfig{Theme: "ocean", Title: "Ocean"}, pathPreviewPageData{Title: "Live theme", Markdown: "**CONTENT**\n\n![image](assets/a.png)<!-- {\"width\":220,\"height\":110} -->"})
		if err != nil {
			t.Fatal(err)
		}
		return string(html)
	}
	first := render()
	for _, part := range []string{"OCEAN_LAYOUT", "<strong>CONTENT</strong>", `src="assets/a.png"`, `width="220" height="110"`} {
		if !strings.Contains(first, part) {
			t.Fatalf("custom Hugo layout missing %s: %s", part, first)
		}
	}
	writeTestFile(t, filepath.Join(directory, "ocean", "layouts", "_default", "single.html"), `{{ define "main" }}<h1>UPDATED_LAYOUT</h1>{{ .Content }}{{ end }}`)
	if updated := render(); !strings.Contains(updated, "UPDATED_LAYOUT") {
		t.Fatal("refresh retained stale template")
	}
	writeTestFile(t, filepath.Join(directory, "derived", "theme.json"), `{"id":"derived","label":"Derived Stack","extends":"stack"}`)
	themes, err = hugoThemes()
	if err != nil {
		t.Fatal(err)
	}
	for _, theme := range themes {
		if theme.ID == "derived" && theme.Module != stackModule {
			t.Fatal("module inheritance lost in theme selector")
		}
	}
	writeTestFile(t, filepath.Join(directory, "loop", "theme.json"), `{"id":"loop","label":"Loop","extends":"loop"}`)
	if _, err = hugoThemeChain("loop"); err == nil {
		t.Fatal("inheritance cycle accepted")
	}
	if _, err = readHugoTheme("../default"); err == nil {
		t.Fatal("theme path traversal accepted")
	}
	if _, err = fs.Stat(themeFiles(), "missing/theme.json"); !os.IsNotExist(err) {
		t.Fatal("missing themes should not resolve")
	}
}

func TestPreviewThemeLinksKeepOriginalNavigation(t *testing.T) {
	html := []byte(`<a href="/_mdoc/render/theme/">Home</a><a href="/_mdoc/render/theme/post/mdoc-preview/#heading">Title</a><link href="/_mdoc/render/theme/style.css"><a href="next.md">Next</a>`)
	rendered := string(rewritePreviewLinks(html, "theme", "/guide/start.md?theme=stack", ""))
	for _, expected := range []string{`href="/"`, `href="/guide/start.md?theme=stack#heading"`, `href="/_mdoc/render/theme/style.css"`, `href="next.md"`} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("missing navigation %s: %s", expected, rendered)
		}
	}
}
