package main

import (
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	themeassets "mdocman/themes"
)

const previewAssetsPrefix = "/_mdoc/"
const previewThemeDirEnv = "MDOC_PREVIEW_THEME_DIR"

const previewThemeMenu = `<div class="theme-switcher"><button type="button" class="theme-toggle" data-preview-theme-toggle aria-label="切换预览主题" aria-haspopup="menu" aria-expanded="false" aria-controls="preview-theme-menu" title="切换预览主题"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3a9 9 0 1 0 9 9c0-.46-.04-.92-.11-1.36A7 7 0 0 1 13.36 3.1 9 9 0 0 0 12 3Z"></path></svg></button><div id="preview-theme-menu" class="theme-menu" data-preview-theme-menu role="menu" aria-label="预览主题" hidden>{{range .Themes}}<button type="button" class="theme-menu-item" role="menuitemradio" aria-checked="{{if eq .ID "default"}}true{{else}}false{{end}}" data-preview-theme-option="{{.ID}}"><span>{{.Label}}</span><span class="theme-menu-check" aria-hidden="true">✓</span></button>{{end}}</div></div>`

type previewTheme struct {
	ID    string
	Label string
}

var previewThemes = []previewTheme{
	{ID: "default", Label: "默认"},
	{ID: "sepia", Label: "护眼"},
	{ID: "dark", Label: "暗色"},
	{ID: "yahei", Label: "雅黑紧凑"},
}

//go:embed preview-theme.js preview-mermaid.js
var embeddedPreviewScript embed.FS

var css = mustReadPreviewAsset("themes/default.css")

var embeddedPreviewScripts = map[string]struct{}{
	"preview-theme.js":   {},
	"preview-mermaid.js": {},
}

func mustReadPreviewAsset(name string) string {
	content, err := readPreviewAsset(name)
	if err != nil {
		panic(fmt.Sprintf("read embedded preview asset %q: %v", name, err))
	}
	return string(content)
}

func readPreviewAsset(name string) ([]byte, error) {
	if _, ok := embeddedPreviewScripts[name]; ok {
		return fs.ReadFile(embeddedPreviewScript, name)
	}
	if strings.HasPrefix(name, "themes/") {
		themeName := strings.TrimPrefix(name, "themes/")
		if themeDir := strings.TrimSpace(os.Getenv(previewThemeDirEnv)); themeDir != "" {
			return os.ReadFile(filepath.Join(themeDir, themeName))
		}
		return fs.ReadFile(themeassets.Files, themeName)
	}
	return nil, fs.ErrNotExist
}

func isPreviewAsset(name string) bool {
	if _, ok := embeddedPreviewScripts[name]; ok {
		return true
	}
	for _, theme := range previewThemes {
		if name == "themes/"+theme.ID+".css" {
			return true
		}
	}
	return false
}

func servePreviewAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, previewAssetsPrefix)), "/")
	if !isPreviewAsset(name) {
		http.NotFound(w, r)
		return
	}
	content, err := readPreviewAsset(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(content)
}
