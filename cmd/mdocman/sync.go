package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const gitKeychainService = "mdocman-git"

var repositorySyncLock sync.Mutex

type syncConfig struct {
	NotebookID string `json:"notebookId"`
	RemoteURL  string `json:"remoteUrl"`
	Branch     string `json:"branch"`
	Status     string `json:"status"`
	LastError  string `json:"lastError,omitempty"`
	LastSyncAt string `json:"lastSyncAt,omitempty"`
	AutoSync   bool   `json:"autoSync"`
}

type syncManifestDocument struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	FolderID string `json:"folderId"`
	Kind     string `json:"kind"`
}

type syncManifest struct {
	Version   int                    `json:"version"`
	Notebook  string                 `json:"notebookId"`
	Documents []syncManifestDocument `json:"documents"`
}

func syncRepoPath(notebookID string) string {
	return filepath.Join("data", "sync", safeName(notebookID))
}

func validRemote(remote string) bool {
	if remote == "" {
		return false
	}
	return strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "ssh://") || strings.HasPrefix(remote, "git@") || strings.HasPrefix(remote, "/") || strings.HasPrefix(remote, "./") || strings.HasPrefix(remote, "../")
}

func (s *server) loadSyncConfig(notebookID string) (syncConfig, string, error) {
	var config syncConfig
	var auto int
	var account string
	err := s.db.QueryRow(`SELECT notebook_id,remote_url,branch,status,last_error,last_sync_at,auto_sync,credential_account FROM sync_configs WHERE notebook_id=?`, notebookID).Scan(&config.NotebookID, &config.RemoteURL, &config.Branch, &config.Status, &config.LastError, &config.LastSyncAt, &auto, &account)
	config.AutoSync = auto != 0
	return config, account, err
}

func (s *server) syncSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config, _, err := s.loadSyncConfig(r.URL.Query().Get("notebookId"))
		if err == sql.ErrNoRows {
			jsonOut(w, map[string]any{"status": "disconnected"})
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonOut(w, config)
	case http.MethodPost:
		var input struct {
			NotebookID           string `json:"notebookId"`
			RemoteURL            string `json:"remoteUrl"`
			Branch               string `json:"branch"`
			Token                string `json:"token"`
			AutoSync             bool   `json:"autoSync"`
			ConfirmPrivateBackup bool   `json:"confirmPrivateBackup"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.NotebookID == "" || !validRemote(strings.TrimSpace(input.RemoteURL)) {
			http.Error(w, "notebook and a supported Git remote are required", 400)
			return
		}
		if !input.ConfirmPrivateBackup {
			http.Error(w, "confirm that private notes are included in backup history", 400)
			return
		}
		if input.Branch == "" {
			input.Branch = "main"
		}
		account := ""
		if strings.TrimSpace(input.Token) != "" {
			account = "sync:" + input.NotebookID
			if err := keyring.Set(gitKeychainService, account, strings.TrimSpace(input.Token)); err != nil {
				http.Error(w, "could not save Git credential in the OS keychain: "+err.Error(), 500)
				return
			}
		}
		_, err := s.db.Exec(`INSERT INTO sync_configs(notebook_id,remote_url,branch,status,last_error,last_sync_at,auto_sync,credential_account) VALUES(?,?,?,'disconnected','','',?,?) ON CONFLICT(notebook_id) DO UPDATE SET remote_url=excluded.remote_url,branch=excluded.branch,auto_sync=excluded.auto_sync,credential_account=CASE WHEN excluded.credential_account='' THEN sync_configs.credential_account ELSE excluded.credential_account END`, input.NotebookID, strings.TrimSpace(input.RemoteURL), input.Branch, input.AutoSync, account)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		config, _, _ := s.loadSyncConfig(input.NotebookID)
		jsonOut(w, config)
	case http.MethodDelete:
		notebookID := r.URL.Query().Get("notebookId")
		_, account, _ := s.loadSyncConfig(notebookID)
		_, err := s.db.Exec(`DELETE FROM sync_configs WHERE notebook_id=?`, notebookID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if account != "" {
			_ = keyring.Delete(gitKeychainService, account)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func writeGitAskpass(repo string) (string, error) {
	path := filepath.Join(repo, ".mdocman-askpass.sh")
	content := "#!/bin/sh\ncase \"$1\" in\n  *Username*) printf '%s\\n' 'x-access-token' ;;\n  *) printf '%s\\n' \"$MDOCMAN_GIT_TOKEN\" ;;\nesac\n"
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		return "", err
	}
	return path, nil
}

func runGit(ctx context.Context, repo, token string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if token != "" {
		askpass, err := writeGitAskpass(repo)
		if err != nil {
			return "", err
		}
		defer os.Remove(askpass)
		command.Env = append(command.Env, "GIT_ASKPASS="+askpass, "MDOCMAN_GIT_TOKEN="+token)
	}
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

func copySyncFile(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.Size() >= 95<<20 {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copySyncTree(source, destination string) error {
	entries, err := os.ReadDir(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		src := filepath.Join(source, entry.Name())
		dst := filepath.Join(destination, entry.Name())
		if entry.IsDir() {
			if err = copySyncTree(src, dst); err != nil {
				return err
			}
		} else if err = copySyncFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func (s *server) exportSyncProjection(notebookID, repo string) error {
	for _, directory := range []string{"notes", "daily", "templates", "assets"} {
		if err := os.RemoveAll(filepath.Join(repo, directory)); err != nil {
			return err
		}
	}
	rows, err := s.db.Query(`SELECT id,COALESCE(folder_id,''),title,content FROM documents WHERE notebook_id=? AND trashed=0 ORDER BY position`, notebookID)
	if err != nil {
		return err
	}
	manifest := syncManifest{Version: 1, Notebook: notebookID, Documents: []syncManifestDocument{}}
	for rows.Next() {
		var id, folderID, title, content string
		if err = rows.Scan(&id, &folderID, &title, &content); err != nil {
			rows.Close()
			return err
		}
		kind := "note"
		relative := filepath.Join("notes", safeName(id)+".md")
		if strings.HasPrefix(id, "daily-") {
			kind = "daily"
			relative = filepath.Join("daily", strings.TrimPrefix(id, "daily-")+".md")
		}
		if err = os.MkdirAll(filepath.Dir(filepath.Join(repo, relative)), 0755); err != nil {
			rows.Close()
			return err
		}
		if err = os.WriteFile(filepath.Join(repo, relative), []byte(content), 0644); err != nil {
			rows.Close()
			return err
		}
		manifest.Documents = append(manifest.Documents, syncManifestDocument{ID: id, Title: title, FolderID: folderID, Kind: kind})
	}
	rows.Close()
	templates, err := s.db.Query(`SELECT id,title,content FROM templates WHERE notebook_id=? ORDER BY title`, notebookID)
	if err == nil {
		for templates.Next() {
			var id, title, content string
			if templates.Scan(&id, &title, &content) == nil {
				relative := filepath.Join("templates", safeName(id)+".md")
				_ = os.MkdirAll(filepath.Dir(filepath.Join(repo, relative)), 0755)
				_ = os.WriteFile(filepath.Join(repo, relative), []byte(content), 0644)
				manifest.Documents = append(manifest.Documents, syncManifestDocument{ID: id, Title: title, Kind: "template"})
			}
		}
		templates.Close()
	}
	if err = copySyncTree("data/uploads", filepath.Join(repo, "assets", "uploads")); err != nil {
		return err
	}
	if err = copySyncTree("data/audio-memos", filepath.Join(repo, "assets", "audio-memos")); err != nil {
		return err
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.MkdirAll(filepath.Join(repo, ".mdocman"), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repo, ".mdocman", "manifest.json"), manifestBytes, 0644)
}

func titleFromSyncedMarkdown(content, fallback string) string {
	match := regexp.MustCompile(`(?m)^#\s+(.+)$`).FindStringSubmatch(content)
	if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
		return strings.TrimSpace(match[1])
	}
	return fallback
}

func (s *server) importSyncProjection(notebookID, repo string) error {
	manifestBytes, err := os.ReadFile(filepath.Join(repo, ".mdocman", "manifest.json"))
	if err != nil {
		return nil
	}
	var manifest syncManifest
	if json.Unmarshal(manifestBytes, &manifest) != nil {
		return fmt.Errorf("the backup manifest needs review before it can be imported")
	}
	var defaultFolder string
	if err = s.db.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY position LIMIT 1`, notebookID).Scan(&defaultFolder); err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range manifest.Documents {
		if item.Kind == "template" {
			content, readErr := os.ReadFile(filepath.Join(repo, "templates", safeName(item.ID)+".md"))
			if readErr == nil {
				_, err = tx.Exec(`INSERT INTO templates(id,notebook_id,title,content,created_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,content=excluded.content`, item.ID, notebookID, item.Title, string(content), now)
			}
			if err != nil {
				return err
			}
			continue
		}
		relative := filepath.Join("notes", safeName(item.ID)+".md")
		if item.Kind == "daily" {
			relative = filepath.Join("daily", strings.TrimPrefix(item.ID, "daily-")+".md")
		}
		content, readErr := os.ReadFile(filepath.Join(repo, relative))
		if readErr != nil {
			continue
		}
		folderID := item.FolderID
		var folderExists int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM folders WHERE id=? AND notebook_id=?`, folderID, notebookID).Scan(&folderExists)
		if folderExists == 0 {
			folderID = defaultFolder
		}
		title := titleFromSyncedMarkdown(string(content), item.Title)
		private := frontmatterPrivate(string(content))
		_, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,999995,?,?,0,0,?,'[]',0) ON CONFLICT(id) DO UPDATE SET title=excluded.title,content=excluded.content,updated_at=excluded.updated_at,private=excluded.private,revision=documents.revision+1`, item.ID, notebookID, folderID, title, string(content), now, now, private)
		if err != nil {
			return err
		}
	}
	if err = copySyncTree(filepath.Join(repo, "assets", "uploads"), "data/uploads"); err != nil {
		return err
	}
	if err = copySyncTree(filepath.Join(repo, "assets", "audio-memos"), "data/audio-memos"); err != nil {
		return err
	}
	return tx.Commit()
}

func hasGitConflicts(repo string) bool {
	conflict := false
	for _, root := range []string{"notes", "daily", "templates"} {
		_ = filepath.WalkDir(filepath.Join(repo, root), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || conflict {
				return nil
			}
			content, _ := os.ReadFile(path)
			if bytesContainsConflict(content) {
				conflict = true
			}
			return nil
		})
	}
	return conflict
}

func bytesContainsConflict(content []byte) bool {
	text := string(content)
	return strings.Contains(text, "<<<<<<< ") && strings.Contains(text, "=======") && strings.Contains(text, ">>>>>>> ")
}

func mergeManifestSides(repo, ours, theirs string) error {
	var left, right syncManifest
	if json.Unmarshal([]byte(ours), &left) != nil || json.Unmarshal([]byte(theirs), &right) != nil {
		return fmt.Errorf("backup metadata conflict could not be reconciled")
	}
	merged := syncManifest{Version: 1, Notebook: left.Notebook, Documents: []syncManifestDocument{}}
	if merged.Notebook == "" {
		merged.Notebook = right.Notebook
	}
	byID := map[string]syncManifestDocument{}
	for _, item := range left.Documents {
		byID[item.Kind+"\x00"+item.ID] = item
	}
	for _, item := range right.Documents {
		if _, exists := byID[item.Kind+"\x00"+item.ID]; !exists {
			byID[item.Kind+"\x00"+item.ID] = item
		}
	}
	for _, item := range byID {
		merged.Documents = append(merged.Documents, item)
	}
	merged.Documents = sortedManifestDocuments(merged.Documents)
	encoded, _ := json.MarshalIndent(merged, "", "  ")
	return os.WriteFile(filepath.Join(repo, ".mdocman", "manifest.json"), encoded, 0644)
}

func (s *server) performSync(ctx context.Context, config syncConfig, account string) (syncConfig, error) {
	repositorySyncLock.Lock()
	defer repositorySyncLock.Unlock()
	repo := syncRepoPath(config.NotebookID)
	if err := os.MkdirAll(repo, 0755); err != nil {
		return config, err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return config, fmt.Errorf("Git is not available on this device")
	}
	token := ""
	if account != "" {
		token, _ = keyring.Get(gitKeychainService, account)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err = runGit(ctx, repo, token, "init", "-b", config.Branch); err != nil {
			if _, fallbackErr := runGit(ctx, repo, token, "init"); fallbackErr != nil {
				return config, err
			}
			_, _ = runGit(ctx, repo, token, "symbolic-ref", "HEAD", "refs/heads/"+config.Branch)
		}
		_, _ = runGit(ctx, repo, token, "config", "user.name", "Mdocman Backup")
		_, _ = runGit(ctx, repo, token, "config", "user.email", "backup@mdocman.local")
	}
	remote, _ := runGit(ctx, repo, token, "remote", "get-url", "origin")
	if remote == "" {
		if _, err := runGit(ctx, repo, token, "remote", "add", "origin", config.RemoteURL); err != nil {
			return config, err
		}
	} else if remote != config.RemoteURL {
		if _, err := runGit(ctx, repo, token, "remote", "set-url", "origin", config.RemoteURL); err != nil {
			return config, err
		}
	}
	if err := s.exportSyncProjection(config.NotebookID, repo); err != nil {
		return config, err
	}
	_, _ = runGit(ctx, repo, token, "add", "--all")
	status, _ := runGit(ctx, repo, token, "status", "--porcelain")
	if status != "" {
		_, _ = runGit(ctx, repo, token, "commit", "-m", "Back up notes "+time.Now().Format("2006-01-02 15:04:05"))
	}
	_, fetchErr := runGit(ctx, repo, token, "fetch", "origin", config.Branch)
	if fetchErr == nil {
		_, mergeErr := runGit(ctx, repo, token, "merge", "--no-edit", "--allow-unrelated-histories", "origin/"+config.Branch)
		if mergeErr != nil {
			ours, oursErr := runGit(ctx, repo, token, "show", ":2:.mdocman/manifest.json")
			theirs, theirsErr := runGit(ctx, repo, token, "show", ":3:.mdocman/manifest.json")
			if oursErr == nil && theirsErr == nil {
				_ = mergeManifestSides(repo, ours, theirs)
			}
			_, _ = runGit(ctx, repo, token, "add", "--all")
			_, _ = runGit(ctx, repo, token, "commit", "-m", "Keep sync conflict markers for review")
		}
		if err := s.importSyncProjection(config.NotebookID, repo); err != nil {
			return config, err
		}
	}
	if _, err := runGit(ctx, repo, token, "push", "-u", "origin", config.Branch); err != nil {
		return config, err
	}
	config.LastSyncAt = time.Now().Format(time.RFC3339Nano)
	config.LastError = ""
	if hasGitConflicts(repo) {
		config.Status = "needs_review"
	} else {
		config.Status = "backed_up"
	}
	_, _ = s.db.Exec(`UPDATE sync_configs SET status=?,last_error='',last_sync_at=? WHERE notebook_id=?`, config.Status, config.LastSyncAt, config.NotebookID)
	return config, nil
}

func (s *server) syncRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	notebookID := r.URL.Query().Get("notebookId")
	config, account, err := s.loadSyncConfig(notebookID)
	if err != nil {
		http.Error(w, "backup is not connected", 400)
		return
	}
	_, _ = s.db.Exec(`UPDATE sync_configs SET status='backing_up',last_error='' WHERE notebook_id=?`, notebookID)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	config, err = s.performSync(ctx, config, account)
	if err != nil {
		status := "failed"
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "network") || strings.Contains(lower, "could not resolve") || strings.Contains(lower, "connection") {
			status = "offline"
		}
		_, _ = s.db.Exec(`UPDATE sync_configs SET status=?,last_error=? WHERE notebook_id=?`, status, err.Error(), notebookID)
		http.Error(w, err.Error(), 502)
		return
	}
	jsonOut(w, config)
}

func sortedManifestDocuments(items []syncManifestDocument) []syncManifestDocument {
	copyItems := append([]syncManifestDocument(nil), items...)
	sort.Slice(copyItems, func(i, j int) bool { return copyItems[i].ID < copyItems[j].ID })
	return copyItems
}
