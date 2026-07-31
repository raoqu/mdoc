package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultDatabaseFile       = "mdocman.db"
	defaultKnowledgeBaseColor = "#5b4cf0"
	workspaceStateFile        = "workspace.json"
)

type databaseManager struct {
	directory   string
	mu          sync.RWMutex
	active      string
	connections map[string]*sql.DB
	colors      map[string]string
}

type knowledgeBaseSummary struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Color      string `json:"color"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

type knowledgeBaseCatalogResponse struct {
	Directory      string                 `json:"directory"`
	Active         string                 `json:"active"`
	KnowledgeBases []knowledgeBaseSummary `json:"knowledgeBases"`
}

type workspaceState struct {
	ActiveDatabase      string            `json:"activeDatabase"`
	KnowledgeBaseColors map[string]string `json:"knowledgeBaseColors,omitempty"`
}

func defaultWorkspaceDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("查找用户目录失败：%w", err)
	}
	return filepath.Join(home, ".mdoc"), nil
}

func newDatabaseManager(directory string) (*databaseManager, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败：%w", err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, fmt.Errorf("设置数据目录权限失败：%w", err)
	}
	manager := &databaseManager{
		directory:   directory,
		connections: make(map[string]*sql.DB),
		colors:      savedKnowledgeBaseColors(directory),
	}

	names, err := databaseFiles(directory)
	if err != nil {
		return nil, err
	}
	preferred := manager.savedActiveDatabase()
	if !containsString(names, preferred) {
		if containsString(names, defaultDatabaseFile) {
			preferred = defaultDatabaseFile
		} else if len(names) > 0 {
			preferred = names[0]
		} else {
			preferred = defaultDatabaseFile
			if err = manager.createFile(preferred); err != nil {
				return nil, err
			}
		}
	}
	if err = manager.activate(preferred); err != nil {
		manager.close()
		return nil, err
	}
	return manager, nil
}

func (m *databaseManager) current() *sql.DB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[m.active]
}

func (m *databaseManager) activeName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

func (m *databaseManager) activate(rawName string) error {
	name, err := normalizeDatabaseName(rawName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activateLocked(name)
}

func (m *databaseManager) activateLocked(name string) error {
	info, err := os.Lstat(filepath.Join(m.directory, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("知识库“%s”不存在", knowledgeBaseLabel(name))
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("知识库“%s”不可用", knowledgeBaseLabel(name))
	}
	if m.connections[name] == nil {
		connection, openErr := openDBAt(filepath.Join(m.directory, name))
		if openErr != nil {
			return fmt.Errorf("打开知识库“%s”失败：%w", knowledgeBaseLabel(name), openErr)
		}
		m.connections[name] = connection
	}
	if err = m.writeStateLocked(name); err != nil {
		return err
	}
	m.active = name
	return nil
}

func (m *databaseManager) createAndActivate(rawName string) error {
	name, err := normalizeDatabaseName(rawName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err = os.Lstat(filepath.Join(m.directory, name)); err == nil {
		return fmt.Errorf("知识库“%s”已存在", knowledgeBaseLabel(name))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = m.createFile(name); err != nil {
		return err
	}
	return m.activateLocked(name)
}

func (m *databaseManager) createFile(name string) error {
	databasePath := filepath.Join(m.directory, name)
	file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("创建知识库“%s”失败：%w", knowledgeBaseLabel(name), err)
	}
	if err = file.Close(); err != nil {
		return err
	}
	connection, err := openDBAt(databasePath)
	if err != nil {
		_ = os.Remove(databasePath)
		return fmt.Errorf("初始化知识库“%s”失败：%w", knowledgeBaseLabel(name), err)
	}
	m.connections[name] = connection
	return nil
}

func (m *databaseManager) catalog() (knowledgeBaseCatalogResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names, err := databaseFiles(m.directory)
	if err != nil {
		return knowledgeBaseCatalogResponse{}, err
	}
	items := make([]knowledgeBaseSummary, 0, len(names))
	for _, name := range names {
		info, statErr := os.Stat(filepath.Join(m.directory, name))
		if statErr != nil {
			return knowledgeBaseCatalogResponse{}, statErr
		}
		color := m.colors[name]
		if color == "" {
			color = defaultKnowledgeBaseColor
		}
		items = append(items, knowledgeBaseSummary{
			Name:       name,
			Label:      knowledgeBaseLabel(name),
			Color:      color,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
		})
	}
	return knowledgeBaseCatalogResponse{
		Directory:      m.directory,
		Active:         m.active,
		KnowledgeBases: items,
	}, nil
}

func (m *databaseManager) setKnowledgeBaseColor(rawName, rawColor string) error {
	name, err := normalizeDatabaseName(rawName)
	if err != nil {
		return err
	}
	color, err := normalizeKnowledgeBaseColor(rawColor)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err = os.Stat(filepath.Join(m.directory, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("知识库“%s”不存在", knowledgeBaseLabel(name))
		}
		return err
	}
	previous := m.colors[name]
	m.colors[name] = color
	if err = m.writeStateLocked(m.active); err != nil {
		if previous == "" {
			delete(m.colors, name)
		} else {
			m.colors[name] = previous
		}
		return err
	}
	return nil
}

func (m *databaseManager) activePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return filepath.Join(m.directory, m.active)
}

func (m *databaseManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, connection := range m.connections {
		_ = connection.Close()
		delete(m.connections, name)
	}
}

func (m *databaseManager) savedActiveDatabase() string {
	state := readWorkspaceState(m.directory)
	name, err := normalizeDatabaseName(state.ActiveDatabase)
	if err != nil {
		return ""
	}
	return name
}

func (m *databaseManager) writeStateLocked(active string) error {
	colors := make(map[string]string, len(m.colors))
	for name, color := range m.colors {
		colors[name] = color
	}
	content, err := json.MarshalIndent(workspaceState{
		ActiveDatabase:      active,
		KnowledgeBaseColors: colors,
	}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(m.directory, ".workspace-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(append(content, '\n'))
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tempName, filepath.Join(m.directory, workspaceStateFile)); err != nil {
		return err
	}
	return nil
}

func databaseFiles(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".db") {
			names = append(names, entry.Name())
		}
	}
	sort.Slice(names, func(left, right int) bool {
		return strings.ToLower(names[left]) < strings.ToLower(names[right])
	})
	return names, nil
}

func normalizeDatabaseName(rawName string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return "", errors.New("知识库名称不能为空")
	}
	if !strings.EqualFold(filepath.Ext(name), ".db") {
		name += ".db"
	}
	if len(name) > 128 || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", errors.New("知识库名称不能包含路径或以点开头")
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", errors.New("知识库名称不能包含控制字符")
		}
	}
	return name, nil
}

func normalizeKnowledgeBaseColor(rawColor string) (string, error) {
	color := strings.ToLower(strings.TrimSpace(rawColor))
	if len(color) != 7 || color[0] != '#' {
		return "", errors.New("知识库颜色无效")
	}
	for _, character := range color[1:] {
		if !unicode.IsDigit(character) &&
			(character < 'a' || character > 'f') {
			return "", errors.New("知识库颜色无效")
		}
	}
	return color, nil
}

func knowledgeBaseLabel(name string) string {
	if name == defaultDatabaseFile {
		return "我的知识库"
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func readWorkspaceState(directory string) workspaceState {
	content, err := os.ReadFile(filepath.Join(directory, workspaceStateFile))
	if err != nil {
		return workspaceState{}
	}
	var state workspaceState
	if json.Unmarshal(content, &state) != nil {
		return workspaceState{}
	}
	return state
}

func savedKnowledgeBaseColors(directory string) map[string]string {
	saved := readWorkspaceState(directory).KnowledgeBaseColors
	colors := make(map[string]string, len(saved))
	for rawName, rawColor := range saved {
		name, nameErr := normalizeDatabaseName(rawName)
		color, colorErr := normalizeKnowledgeBaseColor(rawColor)
		if nameErr == nil && colorErr == nil {
			colors[name] = color
		}
	}
	return colors
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *server) knowledgeBaseCatalog(w http.ResponseWriter, r *http.Request) {
	if s.databases == nil {
		http.Error(w, "知识库切换不可用", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var input struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "知识库选择无效", http.StatusBadRequest)
			return
		}
		if err := s.databases.activate(input.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "知识库名称无效", http.StatusBadRequest)
			return
		}
		if err := s.databases.createAndActivate(input.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case http.MethodPatch:
		var input struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "知识库颜色无效", http.StatusBadRequest)
			return
		}
		if err := s.databases.setKnowledgeBaseColor(input.Name, input.Color); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	catalog, err := s.databases.catalog()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOut(w, catalog)
}

func (s *server) revealKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.databases == nil {
		http.Error(w, "知识库不可用", http.StatusNotFound)
		return
	}
	if err := revealInFileManager(s.databases.activePath()); err != nil {
		http.Error(w, fmt.Sprintf("无法显示知识库：%v", err), http.StatusInternalServerError)
		return
	}
	jsonOut(w, map[string]bool{"ok": true})
}

func revealInFileManager(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", "-R", target)
	case "windows":
		command = exec.Command("explorer", "/select,"+filepath.Clean(target))
	default:
		command = exec.Command("xdg-open", filepath.Dir(target))
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func migrateLegacyWorkspace(workspaceDirectory, legacyDirectory string) error {
	legacyAbsolute, err := filepath.Abs(legacyDirectory)
	if err != nil {
		return err
	}
	workspaceAbsolute, err := filepath.Abs(workspaceDirectory)
	if err != nil {
		return err
	}
	if legacyAbsolute == workspaceAbsolute {
		return nil
	}
	if _, err = os.Stat(legacyAbsolute); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err = os.MkdirAll(workspaceAbsolute, 0700); err != nil {
		return err
	}

	legacyDatabase := filepath.Join(legacyAbsolute, defaultDatabaseFile)
	targetDatabase := filepath.Join(workspaceAbsolute, defaultDatabaseFile)
	if _, targetErr := os.Stat(targetDatabase); errors.Is(targetErr, os.ErrNotExist) {
		if _, sourceErr := os.Stat(legacyDatabase); sourceErr == nil {
			if err = copySQLiteSnapshot(legacyDatabase, targetDatabase); err != nil {
				return fmt.Errorf("迁移旧知识库失败：%w", err)
			}
		}
	}
	for _, name := range []string{"notebooks.json", "uploads", "audio-memos"} {
		if err = copyMissingTree(filepath.Join(legacyAbsolute, name), filepath.Join(workspaceAbsolute, name)); err != nil {
			return fmt.Errorf("迁移旧数据 %q 失败：%w", name, err)
		}
	}
	return nil
}

func copySQLiteSnapshot(sourcePath, targetPath string) error {
	source, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	escapedTarget := strings.ReplaceAll(targetPath, "'", "''")
	if _, err = source.Exec("VACUUM INTO '" + escapedTarget + "'"); err != nil {
		return err
	}
	return os.Chmod(targetPath, 0600)
}

func copyMissingTree(sourcePath, targetPath string) error {
	sourceInfo, err := os.Stat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if sourceInfo.IsDir() {
		if err = os.MkdirAll(targetPath, 0700); err != nil {
			return err
		}
		entries, readErr := os.ReadDir(sourcePath)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if err = copyMissingTree(filepath.Join(sourcePath, entry.Name()), filepath.Join(targetPath, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !sourceInfo.Mode().IsRegular() {
		return nil
	}
	if _, err = os.Stat(targetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, sourceInfo.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
