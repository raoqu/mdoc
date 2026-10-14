// Package hugoengine embeds Hugo's configuration, module, resource and page
// build pipeline in Mdoc. No Hugo command or CLI package is used.
package hugoengine

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/bep/logg"
	"github.com/gohugoio/hugo/common/hugo"
	"github.com/gohugoio/hugo/common/loggers"
	"github.com/gohugoio/hugo/config"
	"github.com/gohugoio/hugo/config/allconfig"
	"github.com/gohugoio/hugo/deps"
	"github.com/gohugoio/hugo/hugofs"
	"github.com/gohugoio/hugo/hugolib"
	"github.com/spf13/fsync"
)

// Hugo keeps a few process-global loggers and caches. Serialize whole engine
// operations so configurations and logs cannot cross publication jobs.
var engineMu sync.Mutex

func Version() string { return hugo.CurrentVersion.String() }
func Extended() bool  { return hugo.IsExtended }

func loadConfig(ctx context.Context, source string, flags config.Provider, log *bytes.Buffer) (*allconfig.Configs, error) {
	flags.Set("workingDir", source)
	flags.Set("cacheDir", filepath.Join(source, ".hugo-cache"))
	flags.Set("module.workspace", "off")
	logger := loggers.New(loggers.Options{Level: logg.LevelWarn, Stdout: log, Stderr: log})
	// The caller's HUGO_* environment must not override the managed source,
	// output paths or publication scope. This never changes process environment.
	return allconfig.LoadConfig(allconfig.ConfigSourceDescriptor{
		Context: ctx, Fs: hugofs.Os, Flags: flags, ConfigDir: "config",
		Environment: "production", Environ: []string{"HUGO_ENVIRONMENT=production"}, Logger: logger,
	})
}

// VendorTheme uses Hugo's own module collection, dependency resolution,
// configuration merge and vendor writer. Only downloading Go modules needs Go.
func VendorTheme(ctx context.Context, source string) (string, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	var log bytes.Buffer
	if err := ctx.Err(); err != nil {
		return "", err
	}
	flags := config.New()
	if proxy := os.Getenv("GOPROXY"); proxy != "" {
		flags.Set("module.proxy", proxy)
	}
	configs, err := loadConfig(ctx, source, flags, &log)
	if err == nil {
		err = configs.ModulesClient.Vendor()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return log.String(), fmt.Errorf("安装 Hugo 主题：%w\n%s", err, log.String())
	}
	return log.String(), nil
}

// Build executes HugoSites in this process, including resource pipelines such
// as Sass, image transformations and JS compilation. Themes are already vendored
// and external programs are denied for builds; no runtime Go or Hugo is needed.
func Build(ctx context.Context, source, destination, baseURL string) (string, error) {
	return build(ctx, source, destination, baseURL, true)
}

// BuildPreview uses the same native theme pipeline without HTML minification.
func BuildPreview(ctx context.Context, source, destination, baseURL string) (string, error) {
	return build(ctx, source, destination, baseURL, false)
}

func build(ctx context.Context, source, destination, baseURL string, minify bool) (string, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	var log bytes.Buffer
	if err := ctx.Err(); err != nil {
		return "", err
	}
	flags := config.New()
	flags.Set("baseURL", baseURL)
	flags.Set("publishDir", destination)
	flags.Set("publishDirStatic", destination)
	flags.Set("publishDirDynamic", destination)
	flags.Set("minify", minify)
	flags.Set("module.proxy", "off")
	flags.Set("security.exec.allow", []string{"none"})
	configs, err := loadConfig(ctx, source, flags, &log)
	if err != nil {
		return log.String(), fmt.Errorf("加载 Hugo 配置：%w\n%s", err, log.String())
	}
	flags.Set("publishDir", configs.Base.PublishDir)
	flags.Set("publishDirStatic", configs.Base.PublishDir)
	flags.Set("publishDirDynamic", configs.Base.PublishDir)
	filesystems := hugofs.NewFromSourceAndDestination(hugofs.Os, hugofs.Os, flags)
	sites, err := hugolib.NewHugoSites(deps.DepsCfg{
		Configs: configs, Fs: filesystems, LogOut: &log, LogLevel: logg.LevelWarn,
	})
	if err != nil {
		return log.String(), fmt.Errorf("创建 Hugo 站点：%w\n%s", err, log.String())
	}
	defer sites.Close()
	// Like Hugo's fullBuild, copy the merged static mounts before rendering.
	// The destination is a fresh staging directory, so no destructive sync is needed.
	for _, sourceFS := range sites.BaseFs.SourceFilesystems.Static {
		if err = ctx.Err(); err != nil {
			return log.String(), err
		}
		syncer := fsync.NewSyncer()
		syncer.NoTimes, syncer.NoChmod = true, true
		syncer.SrcFs = sourceFS.Fs
		syncer.DestFs = filesystems.PublishDirStatic
		folder := string(filepath.Separator)
		if sourceFS.PublishFolder != "" {
			folder = filepath.Join(folder, sourceFS.PublishFolder)
		}
		if err = syncer.Sync(folder, string(filepath.Separator)); err != nil && !os.IsNotExist(err) {
			return log.String(), fmt.Errorf("复制 Hugo 静态资源：%w", err)
		}
	}
	fmt.Fprintf(&log, "Mdoc 内置 Hugo %s\n", Version())
	err = sites.Build(hugolib.BuildCfg{Context: ctx})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return log.String(), fmt.Errorf("Hugo 构建失败：%w\n%s", err, log.String())
	}
	sites.PrintProcessingStats(&log)
	// Hugo source files are private; its output must also work with an ordinary
	// unprivileged static server when served outside rsync.
	err = filepath.WalkDir(destination, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if entry.IsDir() {
			mode = 0755
		}
		return os.Chmod(filename, mode)
	})
	return log.String(), err
}
