package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	xhtml "golang.org/x/net/html"

	"mdocman/internal/hugoengine"
)

const previewAssetsPrefix = "/_mdoc/"

// The old CSS URLs remain aliases into native Hugo theme packages.
var css = mustReadPreviewAsset("themes/default.css") + mustReadPreviewAsset("mermaid.css")

func mustReadPreviewAsset(name string) string {
	content, err := readPreviewAsset(name)
	if err != nil {
		panic(fmt.Sprintf("read theme asset %q: %v", name, err))
	}
	return string(content)
}

func readPreviewAsset(name string) ([]byte, error) {
	if name == "preview-theme.js" || name == "preview-mermaid.js" || name == "mermaid.css" {
		return fs.ReadFile(themeFiles(), "_shared/static/mdoc/"+name)
	}
	if strings.HasPrefix(name, "themes/") && strings.HasSuffix(name, ".css") {
		id := strings.TrimSuffix(strings.TrimPrefix(name, "themes/"), ".css")
		if _, err := readHugoTheme(id); err != nil {
			return nil, fs.ErrNotExist
		}
		asset := "style.css"
		if id == "default" {
			asset = "base.css"
		}
		return fs.ReadFile(themeFiles(), id+"/static/mdoc/"+asset)
	}
	return nil, fs.ErrNotExist
}

// Each theme owns a replaceable bundle of generated assets. Page HTML and content
// never enter this store. Hugo fingerprinted resources retain their native URLs.
var previewAssets = struct {
	sync.RWMutex
	bundles map[string]map[string][]byte
}{bundles: map[string]map[string][]byte{}}

func servePreviewAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, previewAssetsPrefix)
	if path.Clean(name) != name || strings.Contains(name, "\\") {
		http.NotFound(w, r)
		return
	}
	var content []byte
	var err error
	if strings.HasPrefix(name, "render/") {
		parts := strings.SplitN(strings.TrimPrefix(name, "render/"), "/", 2)
		if len(parts) == 2 {
			previewAssets.RLock()
			content = previewAssets.bundles[parts[0]][parts[1]]
			previewAssets.RUnlock()
		}
		if content == nil {
			err = fs.ErrNotExist
		}
	} else {
		content, err = readPreviewAsset(name)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}

func previewThemeConfig(w http.ResponseWriter, r *http.Request, config publicationConfig) (publicationConfig, error) {
	id := strings.TrimSpace(r.URL.Query().Get("theme"))
	if id != "" {
		if _, err := readHugoTheme(id); err != nil {
			return config, err
		}
		http.SetCookie(w, &http.Cookie{Name: "mdoc-theme", Value: id, Path: "/", SameSite: http.SameSiteLaxMode, MaxAge: 31536000})
	} else if cookie, err := r.Cookie("mdoc-theme"); err == nil {
		if _, err := readHugoTheme(cookie.Value); err == nil {
			id = cookie.Value
		}
	}
	if id != "" {
		config.Theme = id
	}
	return resolveThemeConfig(config)
}

func (s *server) prepareHugoSource(source string, config publicationConfig) error {
	if config.ThemeModule != "" {
		if !s.publicationThemeInstalled(config) {
			return fmt.Errorf("请先安装 Hugo 主题 %s@%s（mdoc publish theme install）", config.ThemeModule, config.ThemeVersion)
		}
		if err := copyPublicationTree(s.publicationThemeDir(config), source); err != nil {
			return err
		}
	}
	return applyHugoTheme(source, config)
}

func (s *server) renderHugoPreview(ctx context.Context, config publicationConfig, data pathPreviewPageData) ([]byte, error) {
	var err error
	config, err = resolveThemeConfig(config)
	if err != nil {
		return nil, err
	}
	data.Themes, err = hugoThemes()
	if err != nil {
		return nil, err
	}
	// Offer only installed module themes in the local switcher.
	available := data.Themes[:0]
	for _, theme := range data.Themes {
		candidate, err := resolveThemeConfig(publicationConfig{Theme: theme.ID})
		if err != nil {
			return nil, err
		}
		if candidate.ThemeModule == "" || s.publicationThemeInstalled(candidate) {
			available = append(available, theme)
		}
	}
	data.Themes = available
	stage, err := os.MkdirTemp("", "mdoc-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	source := filepath.Join(stage, "source")
	if err = s.prepareHugoSource(source, config); err != nil {
		return nil, err
	}
	// A local file URL is relative to the requested Markdown file, rather than
	// Hugo's temporary post route. Themes can customize these preview partials.
	for _, kind := range []string{"image", "link"} {
		hook := filepath.Join(source, "layouts", "_default", "_markup", "render-"+kind+".html")
		if err = os.MkdirAll(filepath.Dir(hook), 0700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(hook, []byte(`{{ partial "mdoc/preview-`+kind+`.html" . }}`), 0600); err != nil {
			return nil, err
		}
	}
	settings := publicationHugoConfig(config)
	params := settings["params"].(map[string]any)
	params["mdocPreview"] = true
	params["widgets"] = map[string]any{}
	params["mainSections"] = []string{"post"}
	settings["disableKinds"] = []string{"taxonomy", "term", "RSS", "sitemap"}
	if err = writePublicationJSON(filepath.Join(source, "config", "_default", "hugo.json"), settings); err != nil {
		return nil, err
	}
	if err = writePublicationJSON(filepath.Join(source, "data", "mdoc.json"), data); err != nil {
		return nil, err
	}
	metadata := map[string]any{"title": data.Title, "layout": "single", "math": true}
	if data.Directory {
		var listing strings.Builder
		for _, entry := range data.Entries {
			label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ").Replace(entry.Name)
			fmt.Fprintf(&listing, "- [%s](%s)\n", label, entry.URL)
		}
		if len(data.Entries) == 0 {
			listing.WriteString("此目录中没有 Markdown 文件。")
		}
		data.Markdown = listing.String()
	}
	if err = writePreviewContent(filepath.Join(source, "content", "post", "mdoc-preview", "index.md"), metadata, data.Markdown); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(config.Theme+config.ThemeModule+config.ThemeVersion)))[:16]
	destination := filepath.Join(stage, "public")
	if _, err = hugoengine.BuildPreview(ctx, source, destination, "http://localhost"+previewAssetsPrefix+"render/"+key+"/"); err != nil {
		return nil, err
	}
	html, err := os.ReadFile(filepath.Join(destination, "post", "mdoc-preview", "index.html"))
	if err != nil {
		return nil, err
	}
	if data.CurrentURL != "" {
		html = rewritePreviewLinks(html, key, data.CurrentURL, data.HomeURL)
	}
	assets := map[string][]byte{}
	err = filepath.WalkDir(destination, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(destination, filename)
		if err != nil {
			return err
		}
		switch strings.ToLower(filepath.Ext(relative)) {
		case ".html", ".xml", ".json":
			return nil
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		assets[filepath.ToSlash(relative)] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	previewAssets.Lock()
	// Bound process memory when custom themes are frequently switched.
	if len(previewAssets.bundles) >= 32 && previewAssets.bundles[key] == nil {
		clear(previewAssets.bundles)
	}
	previewAssets.bundles[key] = assets
	previewAssets.Unlock()
	return html, nil
}

func writePreviewContent(filename string, metadata map[string]any, body string) error {
	// JSON is a valid Hugo frontmatter format; user frontmatter is never executed.
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	header, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(append(header, '\n'), []byte(prepareImageMetadataForRender(body))...), 0600)
}

func writeHugoPreview(w http.ResponseWriter, r *http.Request, s *server, config publicationConfig, data pathPreviewPageData) {
	data.CurrentURL = r.URL.RequestURI()
	config, err := previewThemeConfig(w, r, config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := s.renderHugoPreview(r.Context(), config, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", hugoPreviewPolicy(html))
	if r.Method != http.MethodHead {
		_, _ = w.Write(html)
	}
}

// Theme-generated home/title links must navigate the original preview server,
// while CSS and compiled resources retain their isolated Hugo asset prefix.
func rewritePreviewLinks(document []byte, key, current, home string) []byte {
	if home == "" {
		home = "/"
	}
	prefix := previewAssetsPrefix + "render/" + key + "/"
	tokens := xhtml.NewTokenizer(bytes.NewReader(document))
	var out bytes.Buffer
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			return out.Bytes()
		}
		raw := append([]byte(nil), tokens.Raw()...)
		if kind == xhtml.StartTagToken {
			token := tokens.Token()
			changed := false
			if token.Data == "a" {
				for i, attr := range token.Attr {
					if attr.Key != "href" {
						continue
					}
					address, err := url.Parse(attr.Val)
					if err != nil || address.Host != "" && address.Host != "localhost" {
						continue
					}
					var target string
					switch address.Path {
					case strings.TrimSuffix(prefix, "/"), prefix:
						target = home
					case prefix + "post/mdoc-preview/":
						target = current
					}
					if target != "" {
						if address.Fragment != "" {
							target += "#" + address.EscapedFragment()
						}
						token.Attr[i].Val = target
						changed = true
					}
				}
			}
			if changed {
				out.WriteString(token.String())
				continue
			}
		}
		out.Write(raw)
	}
}

// Native themes such as Stack contain inline initialization scripts. Authorize
// their exact rendered bytes without enabling arbitrary inline script execution.
func hugoPreviewPolicy(document []byte) string {
	tokens := xhtml.NewTokenizer(bytes.NewReader(document))
	var hashes []string
	inline := false
	for {
		switch tokens.Next() {
		case xhtml.ErrorToken:
			return strings.Replace(previewContentSecurityPolicy, "script-src 'self' https://cdn.jsdelivr.net", "script-src 'self' https://cdn.jsdelivr.net "+strings.Join(hashes, " "), 1)
		case xhtml.StartTagToken:
			token := tokens.Token()
			inline = token.Data == "script"
			for _, attribute := range token.Attr {
				if attribute.Key == "src" {
					inline = false
				}
			}
		case xhtml.TextToken:
			if inline {
				sum := sha256.Sum256(tokens.Text())
				hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
			}
		case xhtml.EndTagToken:
			inline = false
		}
	}
}

// Used by HTML fragment previews; the page shell still comes from the Hugo theme.
func writePreviewPage(w http.ResponseWriter, title string, html template.HTML) {
	writeHugoPreview(w, &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, Header: make(http.Header)}, &server{}, publicationConfig{Theme: "default", Title: title, IncludeTOC: true}, pathPreviewPageData{Title: title, HTML: html})
}
