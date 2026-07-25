package main

import (
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
