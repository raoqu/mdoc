package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type captureTokenInfo struct {
	ID         string `json:"id"`
	NotebookID string `json:"notebookId"`
	Label      string `json:"label"`
	KeyHint    string `json:"keyHint"`
	CreatedAt  string `json:"createdAt"`
	Token      string `json:"token,omitempty"`
}

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *server) captureTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		notebookID := r.URL.Query().Get("notebookId")
		rows, err := s.database().Query(`SELECT id,notebook_id,label,key_hint,created_at FROM capture_tokens WHERE (?='' OR notebook_id=?) ORDER BY created_at DESC`, notebookID, notebookID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		items := []captureTokenInfo{}
		for rows.Next() {
			var item captureTokenInfo
			if rows.Scan(&item.ID, &item.NotebookID, &item.Label, &item.KeyHint, &item.CreatedAt) == nil {
				items = append(items, item)
			}
		}
		jsonOut(w, items)
	case http.MethodPost:
		var input struct {
			NotebookID string `json:"notebookId"`
			Label      string `json:"label"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || strings.TrimSpace(input.NotebookID) == "" {
			http.Error(w, "notebook is required", 400)
			return
		}
		if input.Label == "" {
			input.Label = "浏览器扩展"
		}
		id, err := randomToken()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		token, err := randomToken()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		now := time.Now().Format(time.RFC3339Nano)
		_, err = s.database().Exec(`INSERT INTO capture_tokens(id,notebook_id,label,token_hash,key_hint,created_at) VALUES(?,?,?,?,?,?)`, id, input.NotebookID, strings.TrimSpace(input.Label), tokenDigest(token), keyHint(token), now)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(captureTokenInfo{ID: id, NotebookID: input.NotebookID, Label: input.Label, KeyHint: keyHint(token), CreatedAt: now, Token: token})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) captureToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/capture/tokens/"), "/")
	result, err := s.database().Exec(`DELETE FROM capture_tokens WHERE id=?`, id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		http.Error(w, "capture token not found", 404)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) captureNotebook(r *http.Request) (string, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return "", fmt.Errorf("capture token required")
	}
	token := strings.TrimSpace(auth[len("Bearer "):])
	var notebookID string
	err := s.database().QueryRow(`SELECT notebook_id FROM capture_tokens WHERE token_hash=?`, tokenDigest(token)).Scan(&notebookID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("invalid capture token")
	}
	return notebookID, err
}

func (s *server) saveCaptureScreenshot(dataURL string) (string, error) {
	if dataURL == "" {
		return "", nil
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "data:image/") || !strings.Contains(parts[0], ";base64") {
		return "", fmt.Errorf("invalid screenshot")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(raw) > 12<<20 {
		return "", fmt.Errorf("invalid or oversized screenshot")
	}
	ext := "png"
	if strings.Contains(parts[0], "image/jpeg") {
		ext = "jpg"
	}
	name := fmt.Sprintf("capture-%d.%s", time.Now().UnixNano(), ext)
	if err = os.MkdirAll(s.uploadsDir(), 0700); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(s.uploadsDir(), name), raw, 0600); err != nil {
		return "", err
	}
	return "/uploads/" + name, nil
}

func captureBlock(key, title, pageURL, selection, note, screenshot, capturedAt string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "<!-- mdocman-capture:%s:start -->\n", key)
	fmt.Fprintf(&out, "- [%s](<%s>) · %s\n", strings.ReplaceAll(title, "]", "\\]"), pageURL, capturedAt)
	if selection != "" {
		for _, line := range strings.Split(selection, "\n") {
			fmt.Fprintf(&out, "  > %s\n", line)
		}
	}
	if note != "" {
		fmt.Fprintf(&out, "  - %s\n", strings.ReplaceAll(note, "\n", " "))
	}
	if screenshot != "" {
		fmt.Fprintf(&out, "  ![页面截图](%s)\n", screenshot)
	}
	fmt.Fprintf(&out, "<!-- mdocman-capture:%s:end -->", key)
	return out.String()
}

func upsertCaptureBlock(content, key, block string) string {
	pattern := regexp.MustCompile(`(?s)<!-- mdocman-capture:` + regexp.QuoteMeta(key) + `:start -->.*?<!-- mdocman-capture:` + regexp.QuoteMeta(key) + `:end -->`)
	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, block)
	}
	separator := "\n\n"
	if strings.TrimSpace(content) == "" {
		separator = ""
	}
	if !strings.Contains(content, "## [[Links]]") {
		return strings.TrimRight(content, "\n") + separator + "## [[Links]]\n\n" + block + "\n"
	}
	return strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
}

func (s *server) capture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	notebookID, err := s.captureNotebook(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	var input struct {
		URL              string `json:"url"`
		Title            string `json:"title"`
		Selection        string `json:"selection"`
		Note             string `json:"note"`
		Screenshot       string `json:"screenshot"`
		CapturedAt       string `json:"capturedAt"`
		TargetDocumentID string `json:"targetDocumentId"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "invalid capture envelope", 400)
		return
	}
	parsedURL, parseErr := url.ParseRequestURI(input.URL)
	if parseErr != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		http.Error(w, "a valid page URL is required", 400)
		return
	}
	if input.Title == "" {
		input.Title = input.URL
	}
	if input.CapturedAt == "" {
		input.CapturedAt = time.Now().Format(time.RFC3339)
	}
	screenshot, err := s.saveCaptureScreenshot(input.Screenshot)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	date := time.Now().Format("2006-01-02")
	targetID := input.TargetDocumentID
	if targetID == "" {
		targetID = "daily-" + date
	}
	tx, err := s.database().Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()
	var targetNotebook, targetTitle, targetContent string
	var targetPrivate int
	err = tx.QueryRow(`SELECT notebook_id,title,content,private FROM documents WHERE id=? AND trashed=0`, targetID).Scan(&targetNotebook, &targetTitle, &targetContent, &targetPrivate)
	if err == sql.ErrNoRows && input.TargetDocumentID == "" {
		var folderID string
		err = tx.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,position LIMIT 1`, notebookID, notebookID+"-daily").Scan(&folderID)
		if err == nil {
			targetTitle = date
			targetContent = "# " + date + "\n"
			now := time.Now().Format(time.RFC3339Nano)
			_, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,0,0,0,'[]',0)`, targetID, notebookID, folderID, targetTitle, targetContent, 999999, now, now)
			targetNotebook = notebookID
		}
	}
	if err != nil || targetNotebook != notebookID {
		http.Error(w, "capture target not found in this notebook", 404)
		return
	}
	private := targetPrivate != 0 || frontmatterPrivate(targetContent)
	keyBytes := sha256.Sum256([]byte(date + "\x00" + targetID + "\x00" + input.URL + "\x00" + input.Selection))
	key := hex.EncodeToString(keyBytes[:12])
	block := captureBlock(key, input.Title, input.URL, input.Selection, input.Note, screenshot, input.CapturedAt)
	updated := upsertCaptureBlock(targetContent, key, block)
	now := time.Now().Format(time.RFC3339Nano)
	_, err = tx.Exec(`UPDATE documents SET content=?,updated_at=?,revision=revision+1 WHERE id=?`, updated, now, targetID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var linksCount int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM documents WHERE notebook_id=? AND lower(title)='links' AND trashed=0`, notebookID).Scan(&linksCount)
	if linksCount == 0 {
		var folderID string
		if tx.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY position LIMIT 1`, notebookID).Scan(&folderID) == nil {
			linksID := "links-" + notebookID
			_, _ = tx.Exec(`INSERT OR IGNORE INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,0,0,0,'[]',0)`, linksID, notebookID, folderID, "Links", "# Links\n", 999998, now, now)
		}
	}
	dedicatedID := ""
	if input.Selection != "" || screenshot != "" {
		dedicatedID = "capture-" + key
		var folderID string
		if tx.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY position LIMIT 1`, notebookID).Scan(&folderID) == nil {
			privacyLine := ""
			if private {
				privacyLine = "private: true\n"
			}
			content := fmt.Sprintf("---\nsource: browser-capture\ncapturedUrl: %q\ncapturedAt: %q\ncaptureStatus: done\n%s---\n# %s\n\n%s\n", input.URL, input.CapturedAt, privacyLine, input.Title, block)
			_, _ = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,0,0,?,'[]',0) ON CONFLICT(id) DO UPDATE SET title=excluded.title,content=excluded.content,updated_at=excluded.updated_at,private=excluded.private,revision=documents.revision+1`, dedicatedID, notebookID, folderID, input.Title, content, 999997, now, now, private)
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOut(w, map[string]any{"status": "saved", "targetDocumentId": targetID, "captureDocumentId": dedicatedID, "private": private})
}
