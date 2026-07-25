package main

import (
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
	root  string
	entry string
	md    goldmark.Markdown
}

type pathPreviewEntry struct {
	Name  string
	URL   string
	IsDir bool
}

const pathPreviewDirectoryPage = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta http-equiv="Cache-Control" content="no-store"><title>{{.Title}} · 预览</title><style>{{.CSS}}
.directory-list{list-style:none;padding:0}.directory-list li{border-bottom:1px solid #e5e3dc}.directory-list a{display:block;padding:10px 4px}.directory-list a:hover{background:#f0f3ed}</style></head><body><div class="layout"><main><article><h1>{{.Title}}</h1><ul class="directory-list">{{range .Entries}}<li><a href="{{.URL}}">{{if .IsDir}}📁 {{else}}📄 {{end}}{{.Name}}</a></li>{{else}}<li>此目录中没有 Markdown 文件。</li>{{end}}</ul></article></main></div><footer>由本地 Mdocman 服务实时渲染</footer></body></html>`

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
	if !info.IsDir() {
		if !isMarkdownPath(absolute) {
			return nil, fmt.Errorf("%q 不是 Markdown 文件", target)
		}
		root = filepath.Dir(absolute)
		entry = filepath.Base(absolute)
	}
	return &pathPreviewServer{root: root, entry: entry, md: md}, nil
}

func (p *pathPreviewServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	title := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	renderer := &server{md: p.md}
	writePreviewPage(w, title, renderer.render(markdownBody(string(content))))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", previewContentSecurityPolicy)
	t := template.Must(template.New("directory").Parse(pathPreviewDirectoryPage))
	if err := t.Execute(w, map[string]any{"Title": title, "Entries": entries, "CSS": template.CSS(css)}); err != nil {
		log.Printf("directory preview render: %v", err)
	}
}

func escapedPathSegment(segment string) string {
	return (&url.URL{Path: segment}).EscapedPath()
}

func markdownIndex(directory string) string {
	for _, name := range []string{"README.md", "readme.md", "index.md", "INDEX.md"} {
		candidate := filepath.Join(directory, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
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
