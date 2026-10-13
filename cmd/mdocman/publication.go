package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"mdocman/internal/hugoengine"
)

const stackModule = "github.com/CaiJimmy/hugo-theme-stack/v3"

type publicationConfig struct {
	Title        string `json:"title"`
	BaseURL      string `json:"baseURL"`
	Subtitle     string `json:"subtitle"`
	ThemeModule  string `json:"themeModule"`
	ThemeVersion string `json:"themeVersion"`
	IncludeTOC   bool   `json:"includeTOC"`
	DeployTarget string `json:"deployTarget"`
}

type publicationResult struct {
	OK         bool   `json:"ok"`
	Documents  int    `json:"documents"`
	Output     string `json:"output"`
	PreviewURL string `json:"previewURL"`
	URL        string `json:"url"`
	Log        string `json:"log"`
}

func defaultPublicationConfig() publicationConfig {
	return publicationConfig{Title: "墨笺文档", BaseURL: "https://mdoc.raoqu.cc/", Subtitle: "公开的笔记与文档", ThemeModule: stackModule, ThemeVersion: "v3.31.0", IncludeTOC: true, DeployTarget: "root@10.6.0.1:/www/wwwroot/mdoc/"}
}

// Each database has its own settings, content projection and output. Freeze the
// database pointer before a long operation, even if the UI switches databases.
func (s *server) publicationScope() (*server, string, error) {
	db := s.database()
	var seq int
	var name, filename string
	err := db.QueryRow("SELECT seq,name,file FROM pragma_database_list WHERE name='main'").Scan(&seq, &name, &filename)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(filename))
	root, err := filepath.Abs(s.workspacePath("publishing", fmt.Sprintf("%x", sum[:8])))
	return &server{db: db, dataDir: s.dataDir}, root, err
}

func readPublicationConfig(root string) (publicationConfig, error) {
	config := defaultPublicationConfig()
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(data, &config)
	if err == nil {
		config.BaseURL = strings.TrimRight(config.BaseURL, "/") + "/"
	}
	return config, err
}

var themeModulePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*(/[a-zA-Z0-9][a-zA-Z0-9._-]*)+$`)
var themeVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)
var deployTargetPattern = regexp.MustCompile(`^(?:[a-zA-Z0-9_][a-zA-Z0-9_.-]*@)?[a-zA-Z0-9][a-zA-Z0-9.-]*:(/[a-zA-Z0-9_./-]+)$`)

func validatePublicationConfig(config publicationConfig) error {
	if strings.TrimSpace(config.Title) == "" {
		return errors.New("请输入站点标题")
	}
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") && u.Path != "" && u.Path != "/" {
		return errors.New("站点 URL 必须是完整的 HTTP(S) 地址，可带子路径，不含查询参数")
	}
	if !themeModulePattern.MatchString(config.ThemeModule) || !strings.Contains(strings.Split(config.ThemeModule, "/")[0], ".") || !themeVersionPattern.MatchString(config.ThemeVersion) {
		return errors.New("主题需使用 Hugo Module 路径及固定版本号（如 v3.31.0）")
	}
	if config.DeployTarget != "" {
		return validateDeployTarget(config.DeployTarget)
	}
	return nil
}

func validateDeployTarget(target string) error {
	match := deployTargetPattern.FindStringSubmatch(target)
	if match == nil {
		return errors.New("部署目标格式应为 user@host:/独立/站点/目录/")
	}
	remote := strings.TrimSuffix(match[1], "/")
	if path.Clean(remote) != remote || len(strings.Split(strings.Trim(remote, "/"), "/")) < 3 {
		return errors.New("部署必须使用至少三级的独立目录，不能同步到服务器根目录或上级目录")
	}
	return nil
}

func writePublicationJSON(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(filename), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filename)
}

func publicationSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(w, "cross-origin publication request denied", http.StatusForbidden)
			return false
		}
	}
	return true
}

// Serialize CLI and HTTP operations across processes as they share theme caches.
func (s *server) lockPublication() (func(), error) {
	directory := s.workspacePath("publishing")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(directory, ".lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("另一个发布任务正在运行，请稍后重试")
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s *server) publicationSettings(w http.ResponseWriter, r *http.Request) {
	if !publicationSameOrigin(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !s.publicationMu.TryLock() {
		http.Error(w, "发布任务正在运行，请稍后重试", 409)
		return
	}
	defer s.publicationMu.Unlock()
	unlock, lockErr := s.lockPublication()
	if lockErr != nil {
		http.Error(w, lockErr.Error(), 409)
		return
	}
	defer unlock()
	scoped, root, err := s.publicationScope()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if expected := r.Header.Get("X-Mdoc-Publication-Directory"); expected != "" && expected != root {
		http.Error(w, "知识库已切换，请重新打开发布设置", http.StatusConflict)
		return
	}
	config, err := readPublicationConfig(root)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodPut {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err = json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		if err = validatePublicationConfig(config); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		config.BaseURL = strings.TrimRight(config.BaseURL, "/") + "/"
		if err = writePublicationJSON(filepath.Join(root, "config.json"), config); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	docs, err := publicationDocuments(scoped.database())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	summaries := make([]map[string]string, 0, len(docs))
	for _, doc := range docs {
		summaries = append(summaries, map[string]string{"id": doc.ID, "title": doc.Title, "url": config.BaseURL + doc.Route})
	}
	jsonOut(w, map[string]any{"config": config, "documents": summaries, "themeInstalled": scoped.publicationThemeInstalled(config), "hugoVersion": hugoengine.Version(), "hugoExtended": hugoengine.Extended(), "directory": root, "previewURL": "/site/"})
}

func (s *server) publicationActions(w http.ResponseWriter, r *http.Request) {
	s.publicationAction(w, r, strings.TrimPrefix(r.URL.Path, "/api/publication/"))
}

func (s *server) publicationAction(w http.ResponseWriter, r *http.Request, action string) {
	if !publicationSameOrigin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if action != "build" && action != "deploy" && action != "theme" {
		http.NotFound(w, r)
		return
	}
	if !s.publicationMu.TryLock() {
		http.Error(w, "发布任务正在运行，请稍后重试", 409)
		return
	}
	defer s.publicationMu.Unlock()
	unlock, lockErr := s.lockPublication()
	if lockErr != nil {
		http.Error(w, lockErr.Error(), 409)
		return
	}
	defer unlock()
	scoped, root, err := s.publicationScope()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if expected := r.Header.Get("X-Mdoc-Publication-Directory"); expected != "" && expected != root {
		http.Error(w, "知识库已切换，请重新打开发布设置", http.StatusConflict)
		return
	}
	config, err := readPublicationConfig(root)
	if err == nil {
		err = validatePublicationConfig(config)
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if action == "theme" {
		output, installErr := scoped.installPublicationTheme(ctx, config)
		if installErr != nil {
			http.Error(w, installErr.Error(), 500)
			return
		}
		jsonOut(w, map[string]any{"ok": true, "themeInstalled": true, "log": output})
		return
	}
	result, err := scoped.runPublication(ctx, root, config, action == "deploy")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOut(w, result)
}

func (s *server) publicationPreview(w http.ResponseWriter, r *http.Request) {
	s.publicationMu.RLock()
	defer s.publicationMu.RUnlock()
	_, root, err := s.publicationScope()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.StripPrefix("/site/", http.FileServer(http.Dir(filepath.Join(root, "current", "preview")))).ServeHTTP(w, r)
}

func publicationCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s 执行失败：%w\n%s", name, err, output)
	}
	return string(output), nil
}

func (s *server) publicationThemeDir(config publicationConfig) string {
	hash := sha256.Sum256([]byte(config.ThemeModule + "@" + config.ThemeVersion))
	dir, _ := filepath.Abs(s.workspacePath("publishing", "themes", fmt.Sprintf("%x", hash[:16])))
	return dir
}

func (s *server) publicationThemeInstalled(config publicationConfig) bool {
	data, err := os.ReadFile(filepath.Join(s.publicationThemeDir(config), "installed.json"))
	var installed publicationConfig
	return err == nil && json.Unmarshal(data, &installed) == nil && installed.ThemeModule == config.ThemeModule && installed.ThemeVersion == config.ThemeVersion
}

func publicationModuleConfig(config publicationConfig) map[string]any {
	// Explicit mounts prevent a theme's example content from joining the site.
	mounts := []map[string]string{}
	for _, component := range []string{"layouts", "assets", "static", "i18n", "data"} {
		mounts = append(mounts, map[string]string{"source": component, "target": component})
	}
	return map[string]any{"imports": []any{map[string]any{"path": config.ThemeModule, "mounts": mounts}}}
}

func (s *server) installPublicationTheme(ctx context.Context, config publicationConfig) (string, error) {
	if err := validatePublicationConfig(config); err != nil {
		return "", err
	}
	if s.publicationThemeInstalled(config) {
		return "主题已安装：" + config.ThemeModule + "@" + config.ThemeVersion, nil
	}
	target := s.publicationThemeDir(config)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp(filepath.Dir(target), ".theme-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	goMod := "module mdoc.local/publication\n\ngo 1.20\n\nrequire " + config.ThemeModule + " " + config.ThemeVersion + "\n"
	if err = os.WriteFile(filepath.Join(temp, "go.mod"), []byte(goMod), 0600); err != nil {
		return "", err
	}
	if err = writePublicationJSON(filepath.Join(temp, "config", "_default", "module.json"), publicationModuleConfig(config)); err != nil {
		return "", err
	}
	// Download is an explicit installation step; builds use the vendored copy.
	output, err := hugoengine.VendorTheme(ctx, temp)
	if err != nil {
		return output, err
	}
	if _, err = os.Stat(filepath.Join(temp, "_vendor", "modules.txt")); err != nil {
		return output, fmt.Errorf("主题未生成 vendor 清单：%w", err)
	}
	// The downloaded module cache is not part of an installed theme. Keep only
	// its vendored runtime assets, manifests and configuration.
	if err = os.RemoveAll(filepath.Join(temp, ".hugo-cache")); err != nil {
		return output, err
	}
	if err = writePublicationJSON(filepath.Join(temp, "installed.json"), config); err != nil {
		return output, err
	}
	if err = replacePublicationDirectory(temp, target); err != nil {
		return output, err
	}
	return output + "\n已安装 " + config.ThemeModule + "@" + config.ThemeVersion, nil
}

// Swap only managed output directories. An unsuccessful build leaves the last
// successful preview intact, and never reaches rsync.
func replacePublicationDirectory(source, target string) error {
	previous := target + ".previous"
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		if err = os.Rename(target, previous); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		_ = os.Rename(previous, target)
		return err
	}
	return os.RemoveAll(previous)
}

func copyPublicationTree(source, destination string) error {
	return filepath.WalkDir(source, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, filename)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("发布资源不允许符号链接或特殊文件：%s", filename)
		}
		return copyPublicationFile(filename, target)
	})
}

func copyPublicationFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("发布资源必须为普通文件：%s", source)
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func publicationHugoConfig(config publicationConfig) map[string]any {
	return map[string]any{
		"baseURL": config.BaseURL, "title": config.Title, "languageCode": "zh-CN", "defaultContentLanguage": "zh-cn", "hasCJKLanguage": true,
		"paginate": 20, "pagination": map[string]any{"pagerSize": 20}, "buildDrafts": false, "buildFuture": true,
		"taxonomies": map[string]string{"tag": "tags", "category": "categories"},
		"params": map[string]any{
			"mainSections": []string{"post"}, "rssFullContent": true,
			"sidebar":     map[string]any{"subtitle": config.Subtitle, "avatar": map[string]any{"enabled": false}},
			"article":     map[string]any{"toc": config.IncludeTOC, "math": true, "readingTime": true},
			"comments":    map[string]any{"enabled": false},
			"dateFormat":  map[string]string{"published": "2006-01-02", "lastUpdated": "2006-01-02 15:04"},
			"widgets":     map[string]any{"homepage": []any{map[string]string{"type": "search"}, map[string]string{"type": "categories"}, map[string]string{"type": "tag-cloud"}}, "page": []any{map[string]string{"type": "toc"}}},
			"colorScheme": map[string]any{"toggle": true, "default": "auto"},
		},
		"markup": map[string]any{"goldmark": map[string]any{"renderer": map[string]bool{"unsafe": false}}, "highlight": map[string]any{"noClasses": false}, "tableOfContents": map[string]any{"startLevel": 2, "endLevel": 4}},
	}
}

func (s *server) runPublication(ctx context.Context, root string, config publicationConfig, deploy bool) (publicationResult, error) {
	result := publicationResult{PreviewURL: "/site/", URL: config.BaseURL}
	if err := validatePublicationConfig(config); err != nil {
		return result, err
	}
	if deploy {
		if err := validateDeployTarget(config.DeployTarget); err != nil {
			return result, err
		}
	}
	if !s.publicationThemeInstalled(config) {
		return result, errors.New("请先单独安装所选主题（设置 → 文档发布 → 安装主题，或 mdoc publish theme install）")
	}
	docs, err := publicationDocuments(s.database())
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(root, ".build-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	source := filepath.Join(stage, "source")
	if err = copyPublicationTree(s.publicationThemeDir(config), source); err != nil {
		return result, err
	}
	if err = writePublicationJSON(filepath.Join(source, "config", "_default", "hugo.json"), publicationHugoConfig(config)); err != nil {
		return result, err
	}
	// Stack exposes a footer hook; keep Mermaid code fences usable on the site.
	if config.ThemeModule == stackModule {
		extensions := map[string]string{
			"layouts/_default/_markup/render-codeblock-mermaid.html": "<pre class=\"mermaid\">{{ .Inner }}</pre>{{ .Page.Store.Set \"mdocMermaid\" true }}",
			"layouts/partials/footer/custom.html":                    `{{ if .Store.Get "mdocMermaid" }}<script type="module">import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs"; mermaid.initialize({startOnLoad:true,securityLevel:"strict",theme:document.documentElement.dataset.scheme === "dark" ? "dark" : "neutral"});</script>{{ end }}`,
		}
		for filename, content := range extensions {
			destination := filepath.Join(source, filepath.FromSlash(filename))
			if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return result, err
			}
			if err = os.WriteFile(destination, []byte(content), 0600); err != nil {
				return result, err
			}
		}
	}
	// Native Hugo overrides are separate from both the app and the installed theme.
	for _, component := range []string{"layouts", "assets", "i18n"} {
		override := filepath.Join(root, "overrides", component)
		if _, statErr := os.Stat(override); statErr == nil {
			if err = copyPublicationTree(override, filepath.Join(source, component)); err != nil {
				return result, err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return result, statErr
		}
	}
	if err = s.exportPublication(source, docs); err != nil {
		return result, err
	}
	release := filepath.Join(stage, "release")
	for _, build := range []struct{ directory, baseURL string }{{"public", config.BaseURL}, {"preview", "http://localhost/site/"}} {
		output, buildErr := hugoengine.Build(ctx, source, filepath.Join(release, build.directory), build.baseURL)
		result.Log += output
		if buildErr != nil {
			return result, buildErr
		}
		if _, err = os.Stat(filepath.Join(release, build.directory, "index.html")); err != nil {
			return result, errors.New("Hugo 未生成站点首页，请检查主题")
		}
	}
	if err = writePublicationJSON(filepath.Join(release, "manifest.json"), map[string]any{"builtAt": time.Now().UTC(), "documents": len(docs), "config": config}); err != nil {
		return result, err
	}
	if err = replacePublicationDirectory(release, filepath.Join(root, "current")); err != nil {
		return result, err
	}
	result.Documents = len(docs)
	result.Output = filepath.Join(root, "current", "public")
	if deploy {
		output, deployErr := publicationCommand(ctx, root, "rsync", "-az", "--checksum", "--delete-delay", "--delay-updates", "--safe-links", "--chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r", "-e", "ssh -o BatchMode=yes -o ConnectTimeout=15", "--", result.Output+string(os.PathSeparator), strings.TrimRight(config.DeployTarget, "/")+"/")
		result.Log += output
		if deployErr != nil {
			return result, deployErr
		}
	}
	result.OK = true
	return result, nil
}
