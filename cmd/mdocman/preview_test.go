package main

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark"
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
