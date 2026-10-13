package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

type publicationDocument struct {
	Doc
	Route    string
	Metadata map[string]any
	Body     string
}

func publicationDocuments(db *sql.DB) ([]publicationDocument, error) {
	rows, err := db.Query(`SELECT id,title,content,created_at,updated_at,private,trashed FROM documents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []publicationDocument{}
	for rows.Next() {
		var doc publicationDocument
		if err = rows.Scan(&doc.ID, &doc.Title, &doc.Content, &doc.CreatedAt, &doc.UpdatedAt, &doc.Private, &doc.Trashed); err != nil {
			return nil, err
		}
		if doc.Private || doc.Trashed || frontmatterPrivate(doc.Content) {
			continue
		}
		raw, body, ok := splitMarkdownFrontmatter(doc.Content)
		if !ok {
			continue
		}
		metadata := map[string]any{}
		// Invalid YAML or a string such as publish: "true" never opts a note in.
		if yaml.Unmarshal([]byte(raw), &metadata) != nil || metadata["publish"] != true || metadata["draft"] != nil && metadata["draft"] != false {
			continue
		}
		if private, exists := metadata["private"]; exists && private != false && private != nil {
			continue
		}
		doc.Metadata, doc.Body = metadata, body
		hash := sha256.Sum256([]byte(doc.ID))
		doc.Route = fmt.Sprintf("post/%x/", hash[:12])
		if title, ok := metadata["title"].(string); ok && strings.TrimSpace(title) != "" {
			doc.Title = title
		}
		result = append(result, doc)
	}
	return result, rows.Err()
}

var publicationWikiPattern = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~|`[^`]*`|!?\\[\\[([^\\]\\n]+)\\]\\]")

func publicationWikiLinks(source string, docs []publicationDocument) string {
	targets := map[string]string{}
	add := func(key, route string) {
		key = strings.ToLower(strings.TrimSpace(key))
		if old, exists := targets[key]; exists && old != route {
			targets[key] = ""
		} else if !exists {
			targets[key] = route
		}
	}
	for _, doc := range docs {
		add(doc.ID, doc.Route)
		add(doc.Title, doc.Route)
		if aliases, ok := doc.Metadata["aliases"].([]any); ok {
			for _, alias := range aliases {
				if name, ok := alias.(string); ok {
					add(name, doc.Route)
				}
			}
		}
	}
	return publicationWikiPattern.ReplaceAllStringFunc(source, func(match string) string {
		parts := publicationWikiPattern.FindStringSubmatch(match)
		if len(parts) < 2 || parts[1] == "" {
			return match
		}
		target, label, aliased := strings.Cut(parts[1], "|")
		if !aliased {
			label = target
		}
		target, heading, _ := strings.Cut(target, "#")
		label = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;").Replace(label)
		route := targets[strings.ToLower(strings.TrimSpace(target))]
		if route == "" {
			return label
		}
		ref := route + "index.md"
		if heading != "" {
			ref += "#" + slug(heading)
		}
		return fmt.Sprintf("[%s]({{< relref %q >}})", label, ref)
	})
}

func publicationDate(value any, fallback string) string {
	candidates := []string{fallback}
	if date, ok := value.(time.Time); ok {
		return date.Format(time.RFC3339)
	}
	if date, ok := value.(string); ok {
		candidates = append([]string{date}, candidates...)
	}
	for _, candidate := range candidates {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if date, err := time.Parse(layout, candidate); err == nil {
				return date.Format(time.RFC3339)
			}
		}
	}
	return "1970-01-01T00:00:00Z"
}

// Only attachment URLs actually referenced by selected notes are copied. Bundle
// attachments so Hugo supplies correct URLs under both /site/ and a base subpath.
func (s *server) publicationAsset(source, bundle string) (string, error) {
	u, err := url.Parse(source)
	if err != nil {
		return source, err
	}
	if u.Host != "" || u.Scheme != "" {
		return source, nil
	}
	var directory, prefix string
	switch {
	case strings.HasPrefix(u.Path, "/uploads/"):
		directory, prefix = s.uploadsDir(), "/uploads/"
	case strings.HasPrefix(u.Path, "/audio/"):
		directory, prefix = s.audioMemosDir(), "/audio/"
	default:
		return source, nil
	}
	name := strings.TrimPrefix(u.Path, prefix)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("无效附件路径：%s", source)
	}
	relative := "attachments/" + strings.Trim(prefix, "/") + "/" + name
	if err = copyPublicationFile(filepath.Join(directory, name), filepath.Join(bundle, filepath.FromSlash(relative))); err != nil {
		return "", fmt.Errorf("附件 %s：%w", name, err)
	}
	u.Path = relative
	u.RawPath = ""
	return u.String(), nil
}

func (s *server) exportPublication(source string, docs []publicationDocument) error {
	md := newMarkdown()
	for _, doc := range docs {
		bundle := filepath.Join(source, "content", filepath.FromSlash(doc.Route))
		if err := os.MkdirAll(bundle, 0700); err != nil {
			return err
		}
		body := publicationWikiLinks(prepareImageMetadataForRender(doc.Body), docs)
		tree := md.Parser().Parse(text.NewReader([]byte(body)))
		replacements := map[string]string{}
		err := ast.Walk(tree, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			var destination string
			switch node := node.(type) {
			case *ast.Image:
				destination = string(node.Destination)
			case *ast.Link:
				destination = string(node.Destination)
			default:
				return ast.WalkContinue, nil
			}
			replacement, err := s.publicationAsset(destination, bundle)
			if err != nil {
				return ast.WalkStop, err
			}
			if replacement != destination {
				replacements[destination] = replacement
			}
			return ast.WalkContinue, nil
		})
		if err != nil {
			return fmt.Errorf("文档 %s：%w", doc.Title, err)
		}
		originals := make([]string, 0, len(replacements))
		for original := range replacements {
			originals = append(originals, original)
		}
		sort.Slice(originals, func(i, j int) bool { return len(originals[i]) > len(originals[j]) })
		for _, original := range originals {
			body = strings.ReplaceAll(body, original, replacements[original])
		}
		metadata := map[string]any{"title": doc.Title, "date": publicationDate(doc.Metadata["date"], doc.CreatedAt), "lastmod": publicationDate(nil, doc.UpdatedAt), "draft": false}
		// Do not pass arbitrary Hugo frontmatter through: url/aliases/_build can
		// expose extra routes or resources, and private metadata is not public.
		for _, key := range []string{"description", "tags", "categories", "keywords", "weight", "math", "toc", "license"} {
			if value, ok := doc.Metadata[key]; ok {
				metadata[key] = value
			}
		}
		if image, ok := doc.Metadata["image"].(string); ok && image != "" {
			asset, err := s.publicationAsset(image, bundle)
			if err != nil {
				return err
			}
			metadata["image"] = asset
		}
		header, err := yaml.Marshal(metadata)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(bundle, "index.md"), []byte("---\n"+string(header)+"---\n"+body), 0600); err != nil {
			return err
		}
	}
	// Stack's search JSON is generated from this same filtered content tree.
	for name, content := range map[string]string{
		"search":   "---\ntitle: 搜索\nlayout: search\noutputs: [html, json]\nmenu:\n  main:\n    weight: 10\n    params:\n      icon: search\n---\n",
		"archives": "---\ntitle: 归档\nlayout: archives\nmenu:\n  main:\n    weight: 20\n    params:\n      icon: archives\n---\n",
	} {
		directory := filepath.Join(source, "content", "page", name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, "index.md"), []byte(content), 0600); err != nil {
			return err
		}
	}
	return nil
}
