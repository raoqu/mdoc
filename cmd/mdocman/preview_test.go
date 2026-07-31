package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

func TestImageMetadataPreview(t *testing.T) {
	source := `![cover](/uploads/cover.png)<!-- {"width":522,"height":294,"href":"https://example.com"} -->`
	prepared := prepareImageMetadataForRender(source)
	if !strings.Contains(prepared, `[![cover](/uploads/cover.png "mdocman-size-522x294")](https://example.com)`) {
		t.Fatalf("prepared markdown = %q", prepared)
	}
	server := &server{md: goldmark.New()}
	rendered := string(server.render(source))
	for _, expected := range []string{
		`<a href="https://example.com">`,
		`<img src="/uploads/cover.png" alt="cover"`,
		`width="522" height="294"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered preview missing %q: %s", expected, rendered)
		}
	}
}

func TestMarkdownBodyRemovesFrontmatterFromPreview(t *testing.T) {
	source := "---\r\nprivate: true\r\naliases:\r\n  - 旧标题\r\n---\r\n# 新标题\r\n\r\n正文"
	body := markdownBody(source)
	if strings.Contains(body, "aliases") || strings.Contains(body, "旧标题") {
		t.Fatalf("frontmatter leaked into preview body: %q", body)
	}
	if !strings.Contains(body, "# 新标题") {
		t.Fatalf("markdown body missing after frontmatter removal: %q", body)
	}
}

func TestMarkdownBodyKeepsUnterminatedFrontmatter(t *testing.T) {
	source := "---\naliases:\n  - 旧标题\n# 正文"
	if got := markdownBody(source); got != source {
		t.Fatalf("unterminated frontmatter changed: %q", got)
	}
}

func TestPreviewPageHasNoHeader(t *testing.T) {
	if strings.Contains(previewPage, "<header") || strings.Contains(previewPage, "Markdown 预览") {
		t.Fatalf("preview header is still present")
	}
}

func TestPreviewPageIncludesThemeSwitcher(t *testing.T) {
	response := httptest.NewRecorder()
	writePreviewPage(response, "Theme preview", template.HTML("<p>Content</p>"))
	body := response.Body.String()
	for _, expected := range []string{
		`data-preview-theme="default"`,
		`href="/_mdoc/themes/default.css"`,
		`id="preview-theme-stylesheet"`,
		`src="/_mdoc/preview-theme.js"`,
		`src="/_mdoc/preview-mermaid.js"`,
		`data-preview-theme-toggle`,
		`aria-label="切换预览主题"`,
		`data-preview-theme-menu`,
		`data-preview-theme-option="default"><span>默认</span>`,
		`data-preview-theme-option="sepia"><span>护眼</span>`,
		`data-preview-theme-option="dark"><span>暗色</span>`,
		`data-preview-theme-option="yahei"><span>雅黑紧凑</span>`,
		`class="theme-menu-check" aria-hidden="true">✓</span>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("preview theme UI missing %q: %s", expected, body)
		}
	}
	for _, unexpected := range []string{"<select", "<option", ">主题</label>"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("preview theme UI still contains dropdown markup %q: %s", unexpected, body)
		}
	}
	policy := response.Header().Get("Content-Security-Policy")
	for _, expected := range []string{
		"script-src 'self'",
		"https://cdn.jsdelivr.net",
		"connect-src https://cdn.jsdelivr.net",
	} {
		if !strings.Contains(policy, expected) {
			t.Fatalf("preview CSP missing %q: %q", expected, policy)
		}
	}
}

func TestPreviewHighlightsCodeAndKeepsMermaidFences(t *testing.T) {
	source := "```go\nfmt.Println(\"hi\")\n```\n\n```mermaid\ngraph LR\n  A --> B\n```\n"
	server := &server{md: newMarkdown()}
	rendered := string(server.render(source))
	for _, expected := range []string{
		`class="chroma"`,
		`<code class="language-mermaid">`,
		`fmt`,
		`Println`,
		`graph LR`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("highlighted preview missing %q: %s", expected, rendered)
		}
	}
	// Mermaid must remain a plain fenced block so the client script can find it.
	if strings.Contains(rendered, `language-mermaid`) {
		// Avoid treating mermaid as a highlighted chroma language block.
		if strings.Contains(rendered, `class="chroma"><code class="language-mermaid"`) {
			t.Fatalf("mermaid fence was incorrectly highlighted: %s", rendered)
		}
	}
}

func TestDefaultPreviewThemeDefinesTypographyContract(t *testing.T) {
	for _, expected := range []string{
		"--preview-background:",
		"--preview-text:",
		"--preview-body-font:",
		"--preview-body-line-height:",
		"--preview-h1-font:",
		"--preview-h6-font:",
		"--preview-table-font:",
		"--preview-table-line-height:",
		"--preview-code-font:",
		"--preview-code-line-height:",
		"--preview-quote-font:",
		"--preview-quote-line-height:",
		"text-indent: 2em;",
		".chroma {",
		".chroma .k",
		".mermaid {",
		".mermaid-wrap {",
		".mermaid-expand {",
		".mermaid-lightbox {",
		".mermaid-lightbox-stage {",
	} {
		if !strings.Contains(css, expected) {
			t.Fatalf("default.css missing theme contract %q", expected)
		}
	}
}

func TestPreviewMermaidScriptProvidesLightboxZoom(t *testing.T) {
	script := mustReadPreviewAsset("preview-mermaid.js")
	for _, expected := range []string{
		"mermaid-wrap",
		"mermaid-expand",
		"mermaid-lightbox",
		"data-mermaid-zoom-in",
		"data-mermaid-zoom-out",
		"data-mermaid-zoom-reset",
		"fitToStage",
		"滚轮缩放",
		"wheel",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("preview-mermaid.js missing lightbox/zoom support %q", expected)
		}
	}
}

func TestYaheiPreviewThemeUsesCrossPlatformCompactTypography(t *testing.T) {
	theme := mustReadPreviewAsset("themes/yahei.css")
	for _, expected := range []string{
		`"Microsoft YaHei"`,
		`"PingFang SC"`,
		`"Hiragino Sans GB"`,
		`"Noto Sans CJK SC"`,
		"--preview-body-font-size: 14px",
		"--preview-body-line-height: 16px",
		"--preview-table-line-height: 14px",
		"--preview-code-line-height: 14px",
		"--preview-quote-line-height: 14px",
	} {
		if !strings.Contains(theme, expected) {
			t.Fatalf("yahei.css missing compact typography %q", expected)
		}
	}
}

func TestPreviewThemeAssetsAreEmbedded(t *testing.T) {
	for _, target := range []string{
		"/_mdoc/themes/default.css",
		"/_mdoc/themes/sepia.css",
		"/_mdoc/themes/dark.css",
		"/_mdoc/themes/yahei.css",
		"/_mdoc/preview-theme.js",
		"/_mdoc/preview-mermaid.js",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		servePreviewAsset(response, request)
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("GET %s: status %d, body length %d", target, response.Code, response.Body.Len())
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/_mdoc/themes/missing.css", nil)
	response := httptest.NewRecorder()
	servePreviewAsset(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown preview theme status = %d, want 404", response.Code)
	}
}

func TestPreviewThemeRefreshReadsUpdatedDevelopmentCSS(t *testing.T) {
	themeDir := t.TempDir()
	themeFile := filepath.Join(themeDir, "default.css")
	t.Setenv(previewThemeDirEnv, themeDir)
	writeTestFile(t, themeFile, "/* first version */")

	request := httptest.NewRequest(http.MethodGet, "/_mdoc/themes/default.css", nil)
	first := httptest.NewRecorder()
	servePreviewAsset(first, request)
	if first.Code != http.StatusOK || first.Body.String() != "/* first version */" {
		t.Fatalf("first CSS response: status %d, body %q", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("first CSS Cache-Control = %q, want no-store", got)
	}

	writeTestFile(t, themeFile, "/* updated version */")
	request = httptest.NewRequest(http.MethodGet, "/_mdoc/themes/default.css", nil)
	updated := httptest.NewRecorder()
	servePreviewAsset(updated, request)
	if updated.Code != http.StatusOK || updated.Body.String() != "/* updated version */" {
		t.Fatalf("updated CSS response: status %d, body %q", updated.Code, updated.Body.String())
	}
}

func TestPathPreviewRendersEntryAndRelativeMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "start.md"), "# Start\n\n[下一页](guide/next.md#details)\n")
	writeTestFile(t, filepath.Join(root, "guide", "next.md"), "# Next\n\n## Details\n")

	preview, err := newPathPreviewServer(filepath.Join(root, "start.md"), goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	))
	if err != nil {
		t.Fatal(err)
	}

	entry := serveTestPath(t, preview, "/")
	if !strings.Contains(entry, `href="guide/next.md#details"`) {
		t.Fatalf("entry did not preserve relative markdown link: %s", entry)
	}
	next := serveTestPath(t, preview, "/guide/next.md")
	if !strings.Contains(next, ">Next</h1>") || !strings.Contains(next, `id="details"`) {
		t.Fatalf("linked markdown was not rendered: %s", next)
	}
}

func TestPathPreviewUsesDirectoryReadmeAndServesRelativeAssets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "README.md"), "# Project\n\n![logo](assets/logo.txt)\n")
	writeTestFile(t, filepath.Join(root, "assets", "logo.txt"), "asset-body")

	preview, err := newPathPreviewServer(root, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}

	index := serveTestPath(t, preview, "/")
	if !strings.Contains(index, "<h1>Project</h1>") || !strings.Contains(index, `src="assets/logo.txt"`) {
		t.Fatalf("directory README was not rendered: %s", index)
	}
	if asset := serveTestPath(t, preview, "/assets/logo.txt"); asset != "asset-body" {
		t.Fatalf("relative asset = %q", asset)
	}
}

func TestPathPreviewListsMarkdownFilesWhenDirectoryHasNoIndex(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "hello world.md"), "# Hello\n")
	writeTestFile(t, filepath.Join(root, "ignored.txt"), "ignored")

	preview, err := newPathPreviewServer(root, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	preview.ServeHTTP(response, request)
	body := response.Body.String()
	if !strings.Contains(body, `href="hello%20world.md"`) || strings.Contains(body, "ignored.txt") {
		t.Fatalf("unexpected directory listing: %s", body)
	}
}

func TestPathPreviewDirectoryModeShowsMarkdownTreeWithoutExtensions(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "README.md"), "# Project\n")
	writeTestFile(t, filepath.Join(root, "guide", "getting started.md"), "# Getting started\n")
	writeTestFile(t, filepath.Join(root, "guide", "ignored.txt"), "ignored")
	writeTestFile(t, filepath.Join(root, "empty", "ignored.txt"), "ignored")

	preview, err := newPathPreviewServer(root, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}

	body := serveTestPath(t, preview, "/guide/getting%20started.md")
	for _, expected := range []string{
		`<aside class="path-tree">`,
		`<summary>guide</summary>`,
		`href="/README.md"`,
		`href="/guide/getting%20started.md" class="active" aria-current="page">getting started</a>`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("directory tree missing %q: %s", expected, body)
		}
	}
	for _, unexpected := range []string{">README.md</a>", ">getting started.md</a>", "ignored.txt", "<summary>empty</summary>"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("directory tree unexpectedly contains %q: %s", unexpected, body)
		}
	}
}

func TestPathPreviewSingleFileModeDoesNotShowDirectoryTree(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "entry.md")
	writeTestFile(t, entry, "# Entry\n")
	writeTestFile(t, filepath.Join(root, "sibling.md"), "# Sibling\n")

	preview, err := newPathPreviewServer(entry, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}
	body := serveTestPath(t, preview, "/")
	if strings.Contains(body, `<aside class="path-tree">`) || strings.Contains(body, ">sibling</a>") {
		t.Fatalf("single-file preview unexpectedly showed directory tree: %s", body)
	}
}

func TestPathPreviewServesEmbeddedThemeAssets(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "entry.md")
	writeTestFile(t, entry, "# Entry\n")

	preview, err := newPathPreviewServer(entry, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/_mdoc/themes/dark.css", nil)
	response := httptest.NewRecorder()
	preview.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "--preview-background:") {
		t.Fatalf("path preview theme asset was not served: status %d, body %q", response.Code, response.Body.String())
	}
}

func TestPathPreviewRejectsFilesOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "docs")
	writeTestFile(t, filepath.Join(root, "index.md"), "# Index\n")
	writeTestFile(t, filepath.Join(parent, "secret.md"), "secret")

	preview, err := newPathPreviewServer(root, goldmark.New())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/../secret.md", nil)
	response := httptest.NewRecorder()
	preview.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d, want 404", response.Code)
	}
}

func writeTestFile(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func serveTestPath(t *testing.T, handler http.Handler, target string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", target, response.Code, response.Body.String())
	}
	return response.Body.String()
}
