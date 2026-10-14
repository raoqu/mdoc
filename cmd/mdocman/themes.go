package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	themeassets "mdocman/themes"
)

const previewThemeDirEnv = "MDOC_PREVIEW_THEME_DIR" // compatibility with existing dev.sh

type hugoTheme struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Extends   string `json:"extends,omitempty"`
	Module    string `json:"module,omitempty"`
	Version   string `json:"version,omitempty"`
	TreeTheme string `json:"treeTheme,omitempty"`
}

var themeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func themeFiles() fs.FS {
	directory := strings.TrimSpace(os.Getenv("MDOC_THEME_DIR"))
	if directory == "" {
		directory = strings.TrimSpace(os.Getenv(previewThemeDirEnv))
	}
	if directory != "" {
		return os.DirFS(directory)
	}
	return themeassets.Files
}

func readHugoTheme(id string) (hugoTheme, error) {
	var theme hugoTheme
	if !themeIDPattern.MatchString(id) {
		return theme, fmt.Errorf("无效主题名称：%q", id)
	}
	data, err := fs.ReadFile(themeFiles(), id+"/theme.json")
	if err != nil {
		return theme, fmt.Errorf("读取 Hugo 主题 %s：%w", id, err)
	}
	if err = json.Unmarshal(data, &theme); err != nil {
		return theme, err
	}
	if theme.ID != id || strings.TrimSpace(theme.Label) == "" {
		return theme, fmt.Errorf("主题 %s 的 id 或 label 无效", id)
	}
	if theme.TreeTheme != "" && !themeIDPattern.MatchString(theme.TreeTheme) {
		return theme, fmt.Errorf("无效目录树样式：%s", theme.TreeTheme)
	}
	if theme.Module != "" && (!themeModulePattern.MatchString(theme.Module) || !strings.Contains(strings.Split(theme.Module, "/")[0], ".") || !themeVersionPattern.MatchString(theme.Version)) {
		return theme, fmt.Errorf("主题 %s 必须声明有效的 Hugo Module 和固定版本", id)
	}
	return theme, nil
}

func hugoThemes() ([]hugoTheme, error) {
	entries, err := fs.ReadDir(themeFiles(), ".")
	if err != nil {
		return nil, err
	}
	themes := []hugoTheme{}
	for _, entry := range entries {
		if !entry.IsDir() || !themeIDPattern.MatchString(entry.Name()) {
			continue
		}
		theme, err := readHugoTheme(entry.Name())
		if err != nil {
			return nil, err
		}
		chain, err := hugoThemeChain(theme.ID)
		if err != nil {
			return nil, err
		}
		theme.TreeTheme = "default"
		for _, ancestor := range chain {
			if ancestor.Module != "" {
				theme.Module, theme.Version = ancestor.Module, ancestor.Version
			}
			if ancestor.TreeTheme != "" {
				theme.TreeTheme = ancestor.TreeTheme
			}
		}
		themes = append(themes, theme)
	}
	sort.SliceStable(themes, func(i, j int) bool {
		if (themes[i].ID == "default") != (themes[j].ID == "default") {
			return themes[i].ID == "default"
		}
		return themes[i].ID < themes[j].ID
	})
	return themes, nil
}

// Resolve inheritance once for both consumers; cycles and invalid parents are errors.
func hugoThemeChain(id string) ([]hugoTheme, error) {
	var chain []hugoTheme
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return nil, fmt.Errorf("Hugo 主题继承形成循环：%s", id)
		}
		seen[id] = true
		theme, err := readHugoTheme(id)
		if err != nil {
			return nil, err
		}
		chain = append([]hugoTheme{theme}, chain...)
		id = theme.Extends
	}
	return chain, nil
}

func resolveThemeConfig(config publicationConfig) (publicationConfig, error) {
	if config.Theme == "" {
		return config, nil
	} // existing module configuration
	chain, err := hugoThemeChain(config.Theme)
	if err != nil {
		return config, err
	}
	config.ThemeModule, config.ThemeVersion = "", ""
	for _, theme := range chain {
		if theme.Module != "" {
			config.ThemeModule, config.ThemeVersion = theme.Module, theme.Version
		}
	}
	return config, nil
}

func copyThemeComponent(files fs.FS, source, destination string) error {
	return fs.WalkDir(files, source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("主题不允许符号链接：%s", name)
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(name, source), "/")
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("主题不允许特殊文件：%s", name)
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}

func applyHugoTheme(source string, config publicationConfig) error {
	id := config.Theme
	if id == "" && config.ThemeModule == stackModule {
		id = "stack"
	}
	var chain []hugoTheme
	var err error
	if id != "" {
		chain, err = hugoThemeChain(id)
		if err != nil {
			return err
		}
	}
	files := themeFiles()
	for _, name := range append([]hugoTheme{{ID: "_shared"}}, chain...) {
		for _, component := range []string{"layouts", "static", "assets", "i18n", "data", "config"} {
			path := name.ID + "/" + component
			if _, err = fs.Stat(files, path); os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if err = copyThemeComponent(files, path, filepath.Join(source, component)); err != nil {
				return err
			}
		}
	}
	return nil
}
