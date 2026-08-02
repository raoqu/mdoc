package main

import (
	"crypto/sha256"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
)

type pathPreviewServer struct {
	root          string
	entry         string
	directoryMode bool
	md            goldmark.Markdown
}

type pathPreviewEntry struct {
	Name  string
	URL   string
	IsDir bool
}

type pathPreviewTreeNode struct {
	Name        string
	URL         string
	TreeKey     string
	IsDir       bool
	Active      bool
	NameToggles bool
	Children    []pathPreviewTreeNode
	order       pathPreviewOrder
}

type pathPreviewOrder struct {
	text    string
	number  float64
	numeric bool
}

const pathPreviewPage = `<!doctype html><html lang="zh-CN" data-preview-theme="default"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta http-equiv="Cache-Control" content="no-store"><title>{{.Title}} · 预览</title><link rel="stylesheet" href="/_mdoc/themes/default.css"><link id="preview-theme-stylesheet" rel="stylesheet" href="/_mdoc/themes/default.css"><script defer src="/_mdoc/preview-theme.js"></script><script type="module" src="/_mdoc/preview-mermaid.js"></script></head><body>` + previewThemeMenu + `<div class="layout">{{if .ShowTree}}<aside class="path-tree" data-path-tree-id="{{.TreeID}}">{{template "tree" .Tree}}</aside>{{end}}<main><article>{{if .Directory}}<h1>{{.Title}}</h1><ul class="directory-list">{{range .Entries}}<li><a href="{{.URL}}">{{if .IsDir}}📁 {{else}}📄 {{end}}{{.Name}}</a></li>{{else}}<li>此目录中没有 Markdown 文件。</li>{{end}}</ul>{{else}}{{.HTML}}{{end}}</article></main></div><footer>由本地 Mdocman 服务实时渲染</footer></body></html>
{{define "tree"}}<ul>{{range .}}{{if .IsDir}}{{if .NameToggles}}<li><details class="path-tree-directory-fold" data-path-tree-key="{{.TreeKey}}" open><summary>{{.Name}}</summary>{{template "tree" .Children}}</details></li>{{else}}<li class="path-tree-directory"><div class="path-tree-directory-row">{{if .Children}}<details class="path-tree-directory-toggle" data-path-tree-key="{{.TreeKey}}" open><summary aria-label="展开或折叠 {{.Name}}" title="展开或折叠"></summary></details>{{else}}<span class="path-tree-directory-toggle-spacer"></span>{{end}}{{if .URL}}<a class="path-tree-directory-link{{if .Active}} active{{end}}" href="{{.URL}}"{{if .Active}} aria-current="page"{{end}}>{{.Name}}</a>{{else}}<span class="path-tree-directory-name">{{.Name}}</span>{{end}}</div>{{template "tree" .Children}}</li>{{end}}{{else}}<li><a href="{{.URL}}"{{if .Active}} class="active" aria-current="page"{{end}}>{{.Name}}</a></li>{{end}}{{end}}</ul>{{end}}`

func pathPreviewArgument(args []string) (string, bool) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return "", false
	}
	switch args[0] {
	case "serve", "today", "search", "show", "path", "help":
		return "", false
	default:
		return args[0], true
	}
}

func newPathPreviewServer(target string, md goldmark.Markdown) (*pathPreviewServer, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("解析路径失败：%w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %q：%w", target, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("无法访问 %q：%w", target, err)
	}

	root := absolute
	entry := ""
	directoryMode := info.IsDir()
	if !info.IsDir() {
		if !isMarkdownPath(absolute) {
			return nil, fmt.Errorf("%q 不是 Markdown 文件", target)
		}
		root = filepath.Dir(absolute)
		entry = filepath.Base(absolute)
	}
	return &pathPreviewServer{root: root, entry: entry, directoryMode: directoryMode, md: md}, nil
}

func (p *pathPreviewServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, previewAssetsPrefix) {
		servePreviewAsset(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	relative := filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/"))
	displayPath := r.URL.Path
	if relative == "" && p.entry != "" {
		relative = p.entry
		displayPath = "/"
	}
	target, ok := p.resolve(relative)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		if displayPath != "/" && !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/"+querySuffix(r.URL.RawQuery), http.StatusMovedPermanently)
			return
		}
		if index := markdownIndex(target); index != "" {
			p.renderMarkdownFile(w, index)
			return
		}
		p.renderDirectory(w, target, r.URL.Path)
		return
	}
	if isMarkdownPath(target) {
		p.renderMarkdownFile(w, target)
		return
	}
	http.ServeFile(w, r, target)
}

func querySuffix(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	return "?" + rawQuery
}

func (p *pathPreviewServer) resolve(relative string) (string, bool) {
	candidate := filepath.Join(p.root, strings.TrimLeft(filepath.Clean(relative), `/\`))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(p.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}

func (p *pathPreviewServer) renderMarkdownFile(w http.ResponseWriter, filename string) {
	content, err := os.ReadFile(filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	title := markdownFrontmatterTitle(
		string(content),
		strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)),
	)
	renderer := &server{md: p.md}
	p.writePage(w, pathPreviewPageData{
		Title: title,
		HTML:  renderer.render(markdownBody(string(content))),
	}, filename)
}

func (p *pathPreviewServer) renderDirectory(w http.ResponseWriter, directory, requestPath string) {
	items, err := os.ReadDir(directory)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entries := make([]pathPreviewEntry, 0, len(items))
	for _, item := range items {
		if !item.IsDir() && !isMarkdownPath(item.Name()) {
			continue
		}
		name := item.Name()
		href := escapedPathSegment(name)
		if item.IsDir() {
			href += "/"
		}
		entries = append(entries, pathPreviewEntry{Name: name, URL: href, IsDir: item.IsDir()})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	title := filepath.Base(directory)
	if requestPath == "/" {
		title = filepath.Base(p.root)
	}
	p.writePage(w, pathPreviewPageData{
		Title:     title,
		Directory: true,
		Entries:   entries,
	}, "")
}

type pathPreviewPageData struct {
	Title     string
	TreeID    string
	HTML      template.HTML
	Themes    []previewTheme
	ShowTree  bool
	Directory bool
	Entries   []pathPreviewEntry
	Tree      []pathPreviewTreeNode
}

func (p *pathPreviewServer) writePage(w http.ResponseWriter, data pathPreviewPageData, activeFilename string) {
	data.ShowTree = p.directoryMode
	data.Themes = previewThemes
	if p.directoryMode {
		data.TreeID = fmt.Sprintf("%x", sha256.Sum256([]byte(p.root)))[:16]
		activeRelative := ""
		if activeFilename != "" {
			activeRelative, _ = filepath.Rel(p.root, activeFilename)
		}
		tree, err := p.buildTree(p.root, activeRelative)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data.Tree = tree
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", previewContentSecurityPolicy)
	t := template.Must(template.New("path-preview").Parse(pathPreviewPage))
	if err := t.Execute(w, data); err != nil {
		log.Printf("path preview render: %v", err)
	}
}

func (p *pathPreviewServer) buildTree(directory, activeRelative string) ([]pathPreviewTreeNode, error) {
	items, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	nodes := make([]pathPreviewTreeNode, 0, len(items))
	for _, item := range items {
		if item.Type()&os.ModeSymlink != 0 {
			continue
		}
		itemPath := filepath.Join(directory, item.Name())
		relative, err := filepath.Rel(p.root, itemPath)
		if err != nil {
			return nil, err
		}
		if item.IsDir() {
			children, err := p.buildTree(itemPath, activeRelative)
			if err != nil {
				return nil, err
			}
			name, index, viewable, order := pathPreviewDirectoryMetadata(itemPath, item.Name())
			hasIndex := index != ""
			if len(children) > 0 || hasIndex {
				url := ""
				active := false
				if viewable {
					url = pathPreviewDirectoryURL(relative)
					indexRelative, _ := filepath.Rel(p.root, index)
					active = filepath.Clean(indexRelative) == filepath.Clean(activeRelative)
				}
				nodes = append(nodes, pathPreviewTreeNode{
					Name:        name,
					URL:         url,
					TreeKey:     filepath.ToSlash(relative),
					IsDir:       true,
					Active:      active,
					NameToggles: !viewable,
					Children:    children,
					order:       order,
				})
			}
			continue
		}
		if !isMarkdownPath(item.Name()) {
			continue
		}
		if strings.EqualFold(item.Name(), "index.md") {
			continue
		}
		name := strings.TrimSuffix(item.Name(), filepath.Ext(item.Name()))
		name, order := pathPreviewMarkdownMetadata(itemPath, name, item.Name())
		nodes = append(nodes, pathPreviewTreeNode{
			Name:   name,
			URL:    pathPreviewURL(relative),
			Active: filepath.Clean(relative) == filepath.Clean(activeRelative),
			order:  order,
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return pathPreviewNodeLess(nodes[i], nodes[j])
	})
	return nodes, nil
}

func pathPreviewURL(relative string) string {
	return (&url.URL{Path: "/" + filepath.ToSlash(relative)}).EscapedPath()
}

func pathPreviewDirectoryURL(relative string) string {
	return strings.TrimSuffix(pathPreviewURL(relative), "/") + "/"
}

func escapedPathSegment(segment string) string {
	return (&url.URL{Path: segment}).EscapedPath()
}

func markdownIndex(directory string) string {
	if index := pathPreviewIndex(directory); index != "" {
		return index
	}
	for _, name := range []string{"README.md", "readme.md"} {
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func pathPreviewIndex(directory string) string {
	items, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	for _, item := range items {
		if !item.IsDir() && item.Type()&os.ModeSymlink == 0 && strings.EqualFold(item.Name(), "index.md") {
			return filepath.Join(directory, item.Name())
		}
	}
	return ""
}

func pathPreviewMarkdownMetadata(filename, titleFallback, filenameFallback string) (string, pathPreviewOrder) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return titleFallback, pathPreviewOrder{text: filenameFallback}
	}
	return pathPreviewSourceMetadata(string(content), titleFallback, filenameFallback)
}

func pathPreviewDirectoryMetadata(directory, fallback string) (string, string, bool, pathPreviewOrder) {
	index := pathPreviewIndex(directory)
	if index == "" {
		return fallback, "", false, pathPreviewOrder{text: fallback}
	}
	content, err := os.ReadFile(index)
	if err != nil {
		return fallback, index, false, pathPreviewOrder{text: fallback}
	}
	source := string(content)
	title, order := pathPreviewSourceMetadata(source, fallback, fallback)
	_, body, _ := splitMarkdownFrontmatter(source)
	return title, index, strings.TrimSpace(body) != "", order
}

func pathPreviewSourceMetadata(source, titleFallback, filenameFallback string) (string, pathPreviewOrder) {
	metadata, ok := parseMarkdownFrontmatterMetadata(source)
	if !ok {
		return titleFallback, pathPreviewOrder{text: filenameFallback}
	}
	title := strings.TrimSpace(metadata.Title)
	displayTitle := title
	if displayTitle == "" {
		displayTitle = titleFallback
	}
	if order, ok := pathPreviewSortOrder(metadata.Sort); ok {
		return displayTitle, order
	}
	if title != "" {
		return displayTitle, pathPreviewOrder{text: title}
	}
	return displayTitle, pathPreviewOrder{text: filenameFallback}
}

func pathPreviewSortOrder(value any) (pathPreviewOrder, bool) {
	switch value := value.(type) {
	case string:
		if value = strings.TrimSpace(value); value != "" {
			return pathPreviewOrder{text: value}, true
		}
	case int:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case int8:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case int16:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case int32:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case int64:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case uint:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case uint8:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case uint16:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case uint32:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case uint64:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case float32:
		return pathPreviewOrder{text: fmt.Sprint(value), number: float64(value), numeric: true}, true
	case float64:
		return pathPreviewOrder{text: fmt.Sprint(value), number: value, numeric: true}, true
	}
	return pathPreviewOrder{}, false
}

func pathPreviewNodeLess(left, right pathPreviewTreeNode) bool {
	if left.order.numeric && right.order.numeric && left.order.number != right.order.number {
		return left.order.number < right.order.number
	}
	leftKey := strings.ToLower(left.order.text)
	rightKey := strings.ToLower(right.order.text)
	if leftKey != rightKey {
		return leftKey < rightKey
	}
	return strings.ToLower(left.Name) < strings.ToLower(right.Name)
}

func isMarkdownPath(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func servePathPreview(target string, md goldmark.Markdown, out io.Writer) error {
	preview, err := newPathPreviewServer(target, md)
	if err != nil {
		return err
	}
	port := strings.TrimSpace(os.Getenv("MDOC_PORT"))
	if port == "" {
		port = "0"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return fmt.Errorf("启动预览服务失败：%w", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	previewURL := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)
	fmt.Fprintf(out, "Mdoc 预览：%s\n根目录：%s\n按 Ctrl+C 停止。\n", previewURL, preview.root)
	if os.Getenv("MDOC_NO_BROWSER") == "" {
		if err := openBrowser(previewURL); err != nil {
			fmt.Fprintf(out, "无法自动打开浏览器：%v\n", err)
		}
	}
	return http.Serve(listener, preview)
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
