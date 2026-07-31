package main

import (
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// newMarkdown returns the shared Markdown renderer used by preview, path
// preview, and static site generation. Fenced code blocks use Chroma for
// syntax highlighting; unknown languages (including mermaid) keep a plain
// language- class so client-side Mermaid can find them.
func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
					chromahtml.TabWidth(2),
				),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}
