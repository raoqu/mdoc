package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	_ "modernc.org/sqlite"
)

type Doc struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	UpdatedAt string   `json:"updatedAt"`
	CreatedAt string   `json:"createdAt,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	Trashed   bool     `json:"trashed,omitempty"`
	Private   bool     `json:"private,omitempty"`
	Aliases   []string `json:"aliases,omitempty"`
	Revision  int      `json:"revision"`
}
type Folder struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Open     bool     `json:"open"`
	Docs     []Doc    `json:"docs"`
	Children []Folder `json:"children"`
}
type Notebook struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Accent      string   `json:"accent"`
	Folders     []Folder `json:"folders"`
}
type server struct {
	db *sql.DB
	md goldmark.Markdown
}

func openDB() (*sql.DB, error) {
	os.MkdirAll("data", 0755)
	db, err := sql.Open("sqlite", "data/mdocman.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	schema := `CREATE TABLE IF NOT EXISTS notebooks(id TEXT PRIMARY KEY,title TEXT NOT NULL,description TEXT NOT NULL DEFAULT '',accent TEXT NOT NULL DEFAULT '#4d6b52',position INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS folders(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,parent_id TEXT, title TEXT NOT NULL,position INTEGER NOT NULL DEFAULT 0,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE,FOREIGN KEY(parent_id) REFERENCES folders(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS documents(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,folder_id TEXT,title TEXT NOT NULL,content TEXT NOT NULL DEFAULT '',position INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE,FOREIGN KEY(folder_id) REFERENCES folders(id) ON DELETE SET NULL);
CREATE TABLE IF NOT EXISTS shares(token TEXT PRIMARY KEY,document_id TEXT NOT NULL UNIQUE,created_at TEXT NOT NULL,FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS ai_providers(id TEXT PRIMARY KEY,provider TEXT NOT NULL,label TEXT NOT NULL,model TEXT NOT NULL,key_hint TEXT NOT NULL DEFAULT '',base_url TEXT NOT NULL DEFAULT '',is_default INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chat_conversations(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,title TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS chat_messages(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL,role TEXT NOT NULL,content TEXT NOT NULL,created_at TEXT NOT NULL,FOREIGN KEY(conversation_id) REFERENCES chat_conversations(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS templates(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,title TEXT NOT NULL,content TEXT NOT NULL,created_at TEXT NOT NULL,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS capture_tokens(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,label TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,key_hint TEXT NOT NULL,created_at TEXT NOT NULL,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS audio_memos(id TEXT PRIMARY KEY,notebook_id TEXT NOT NULL,recorded_date TEXT NOT NULL,file_name TEXT NOT NULL,mime_type TEXT NOT NULL,status TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',transcript_document_id TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,FOREIGN KEY(notebook_id) REFERENCES notebooks(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS sync_configs(notebook_id TEXT PRIMARY KEY,remote_url TEXT NOT NULL,branch TEXT NOT NULL DEFAULT 'main',status TEXT NOT NULL DEFAULT 'disconnected',last_error TEXT NOT NULL DEFAULT '',last_sync_at TEXT NOT NULL DEFAULT '',auto_sync INTEGER NOT NULL DEFAULT 1,credential_account TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_folder_parent ON folders(notebook_id,parent_id,position);CREATE INDEX IF NOT EXISTS idx_docs_folder ON documents(notebook_id,folder_id,position);`
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	for _, migration := range []string{
		`ALTER TABLE documents ADD COLUMN created_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE documents ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE documents ADD COLUMN trashed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE documents ADD COLUMN private INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE documents ADD COLUMN aliases_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE documents ADD COLUMN revision INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, migrationErr := db.Exec(migration); migrationErr != nil &&
			!strings.Contains(strings.ToLower(migrationErr.Error()), "duplicate column") {
			return nil, migrationErr
		}
	}
	ftsSchema := `CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(document_id UNINDEXED,title,content,tokenize='unicode61');
CREATE TRIGGER IF NOT EXISTS documents_fts_insert AFTER INSERT ON documents BEGIN
  INSERT INTO documents_fts(document_id,title,content) VALUES(new.id,new.title,new.content);
END;
CREATE TRIGGER IF NOT EXISTS documents_fts_update AFTER UPDATE OF title,content ON documents BEGIN
  DELETE FROM documents_fts WHERE document_id=old.id;
  INSERT INTO documents_fts(document_id,title,content) VALUES(new.id,new.title,new.content);
END;
CREATE TRIGGER IF NOT EXISTS documents_fts_delete AFTER DELETE ON documents BEGIN
  DELETE FROM documents_fts WHERE document_id=old.id;
END;
INSERT INTO documents_fts(document_id,title,content)
SELECT d.id,d.title,d.content FROM documents d
WHERE NOT EXISTS(SELECT 1 FROM documents_fts f WHERE f.document_id=d.id);`
	if _, err = db.Exec(ftsSchema); err != nil {
		return nil, err
	}
	return db, err
}

func (s *server) load() ([]Notebook, error) {
	rows, err := s.db.Query(`SELECT id,title,description,accent FROM notebooks ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	books := []Notebook{}
	for rows.Next() {
		var b Notebook
		if err = rows.Scan(&b.ID, &b.Title, &b.Description, &b.Accent); err != nil {
			return nil, err
		}
		b.Folders, err = s.loadFolders(b.ID)
		if err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	return books, rows.Err()
}
func (s *server) loadFolders(book string) ([]Folder, error) {
	rows, err := s.db.Query(`SELECT id,parent_id,title FROM folders WHERE notebook_id=? ORDER BY position`, book)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type item struct {
		f      Folder
		parent sql.NullString
	}
	all := map[string]*item{}
	order := []string{}
	for rows.Next() {
		x := &item{}
		if err = rows.Scan(&x.f.ID, &x.parent, &x.f.Title); err != nil {
			return nil, err
		}
		x.f.Open = true
		x.f.Docs = []Doc{}
		x.f.Children = []Folder{}
		all[x.f.ID] = x
		order = append(order, x.f.ID)
	}
	docs, err := s.db.Query(`SELECT id,folder_id,title,content,updated_at,created_at,pinned,trashed,private,aliases_json,revision FROM documents WHERE notebook_id=? ORDER BY position`, book)
	if err != nil {
		return nil, err
	}
	defer docs.Close()
	for docs.Next() {
		var d Doc
		var folder sql.NullString
		var pinned, trashed, private int
		var aliasesJSON string
		if err = docs.Scan(&d.ID, &folder, &d.Title, &d.Content, &d.UpdatedAt, &d.CreatedAt, &pinned, &trashed, &private, &aliasesJSON, &d.Revision); err != nil {
			return nil, err
		}
		d.Pinned = pinned != 0
		d.Trashed = trashed != 0
		d.Private = private != 0
		if json.Unmarshal([]byte(aliasesJSON), &d.Aliases) != nil {
			d.Aliases = []string{}
		}
		if folder.Valid && all[folder.String] != nil {
			all[folder.String].f.Docs = append(all[folder.String].f.Docs, d)
		}
	}
	children := map[string][]string{}
	roots := []string{}
	for _, id := range order {
		x := all[id]
		if x.parent.Valid && all[x.parent.String] != nil {
			children[x.parent.String] = append(children[x.parent.String], id)
		} else {
			roots = append(roots, id)
		}
	}
	var build func(string) Folder
	build = func(id string) Folder {
		f := all[id].f
		for _, cid := range children[id] {
			f.Children = append(f.Children, build(cid))
		}
		return f
	}
	out := []Folder{}
	for _, id := range roots {
		out = append(out, build(id))
	}
	return out, nil
}
func (s *server) save(books []Notebook) error {
	type shareRow struct{ token, documentID, createdAt string }
	type templateRow struct{ id, notebookID, title, content, createdAt string }
	type conversationRow struct{ id, notebookID, title, createdAt, updatedAt string }
	type messageRow struct{ id, conversationID, role, content, createdAt string }
	type captureTokenRow struct{ id, notebookID, label, tokenHash, keyHint, createdAt string }
	type audioMemoRow struct{ id, notebookID, date, fileName, mimeType, status, errorText, transcriptID, createdAt string }
	shares := []shareRow{}
	templates := []templateRow{}
	conversations := []conversationRow{}
	messages := []messageRow{}
	captureTokens := []captureTokenRow{}
	audioMemos := []audioMemoRow{}
	if rows, e := s.db.Query(`SELECT token,document_id,created_at FROM shares`); e == nil {
		for rows.Next() {
			var x shareRow
			if rows.Scan(&x.token, &x.documentID, &x.createdAt) == nil {
				shares = append(shares, x)
			}
		}
		rows.Close()
	}
	if rows, e := s.db.Query(`SELECT id,notebook_id,title,content,created_at FROM templates`); e == nil {
		for rows.Next() {
			var x templateRow
			if rows.Scan(&x.id, &x.notebookID, &x.title, &x.content, &x.createdAt) == nil {
				templates = append(templates, x)
			}
		}
		rows.Close()
	}
	if rows, e := s.db.Query(`SELECT id,notebook_id,title,created_at,updated_at FROM chat_conversations`); e == nil {
		for rows.Next() {
			var x conversationRow
			if rows.Scan(&x.id, &x.notebookID, &x.title, &x.createdAt, &x.updatedAt) == nil {
				conversations = append(conversations, x)
			}
		}
		rows.Close()
	}
	if rows, e := s.db.Query(`SELECT id,conversation_id,role,content,created_at FROM chat_messages`); e == nil {
		for rows.Next() {
			var x messageRow
			if rows.Scan(&x.id, &x.conversationID, &x.role, &x.content, &x.createdAt) == nil {
				messages = append(messages, x)
			}
		}
		rows.Close()
	}
	if rows, e := s.db.Query(`SELECT id,notebook_id,label,token_hash,key_hint,created_at FROM capture_tokens`); e == nil {
		for rows.Next() {
			var x captureTokenRow
			if rows.Scan(&x.id, &x.notebookID, &x.label, &x.tokenHash, &x.keyHint, &x.createdAt) == nil {
				captureTokens = append(captureTokens, x)
			}
		}
		rows.Close()
	}
	if rows, e := s.db.Query(`SELECT id,notebook_id,recorded_date,file_name,mime_type,status,error,transcript_document_id,created_at FROM audio_memos`); e == nil {
		for rows.Next() {
			var x audioMemoRow
			if rows.Scan(&x.id, &x.notebookID, &x.date, &x.fileName, &x.mimeType, &x.status, &x.errorText, &x.transcriptID, &x.createdAt) == nil {
				audioMemos = append(audioMemos, x)
			}
		}
		rows.Close()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM notebooks`); err != nil {
		return err
	}
	for bi, b := range books {
		if _, err = tx.Exec(`INSERT INTO notebooks(id,title,description,accent,position) VALUES(?,?,?,?,?)`, b.ID, b.Title, b.Description, b.Accent, bi); err != nil {
			return err
		}
		var put func([]Folder, *string) error
		put = func(fs []Folder, parent *string) error {
			for i, f := range fs {
				if _, err = tx.Exec(`INSERT INTO folders(id,notebook_id,parent_id,title,position) VALUES(?,?,?,?,?)`, f.ID, b.ID, parent, f.Title, i); err != nil {
					return err
				}
				for j, d := range f.Docs {
					updated := d.UpdatedAt
					if updated == "" {
						updated = time.Now().Format(time.RFC3339)
					}
					created := d.CreatedAt
					if created == "" {
						created = updated
					}
					aliasesJSON, _ := json.Marshal(d.Aliases)
					if _, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, b.ID, f.ID, d.Title, d.Content, j, updated, created, d.Pinned, d.Trashed, d.Private, string(aliasesJSON), d.Revision); err != nil {
						return err
					}
				}
				id := f.ID
				if err = put(f.Children, &id); err != nil {
					return err
				}
			}
			return nil
		}
		if err = put(b.Folders, nil); err != nil {
			return err
		}
	}
	for _, x := range shares {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO shares(token,document_id,created_at) VALUES(?,?,?)`, x.token, x.documentID, x.createdAt)
	}
	for _, x := range templates {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO templates(id,notebook_id,title,content,created_at) VALUES(?,?,?,?,?)`, x.id, x.notebookID, x.title, x.content, x.createdAt)
	}
	for _, x := range conversations {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO chat_conversations(id,notebook_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, x.id, x.notebookID, x.title, x.createdAt, x.updatedAt)
	}
	for _, x := range messages {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO chat_messages(id,conversation_id,role,content,created_at) VALUES(?,?,?,?,?)`, x.id, x.conversationID, x.role, x.content, x.createdAt)
	}
	for _, x := range captureTokens {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO capture_tokens(id,notebook_id,label,token_hash,key_hint,created_at) VALUES(?,?,?,?,?,?)`, x.id, x.notebookID, x.label, x.tokenHash, x.keyHint, x.createdAt)
	}
	for _, x := range audioMemos {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO audio_memos(id,notebook_id,recorded_date,file_name,mime_type,status,error,transcript_document_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, x.id, x.notebookID, x.date, x.fileName, x.mimeType, x.status, x.errorText, x.transcriptID, x.createdAt)
	}
	return tx.Commit()
}
func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next(w, r)
	}
}
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func (s *server) notebooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		b, e := s.load()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		jsonOut(w, b)
	case "PUT":
		var b []Notebook
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		if e := s.save(b); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		records, loadErr := s.load()
		if loadErr != nil {
			http.Error(w, loadErr.Error(), 500)
			return
		}
		jsonOut(w, records)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) documentByID(id string) (Doc, error) {
	var d Doc
	var pinned, trashed, private int
	var aliasesJSON string
	err := s.db.QueryRow(`SELECT id,title,content,updated_at,created_at,pinned,trashed,private,aliases_json,revision FROM documents WHERE id=?`, id).
		Scan(&d.ID, &d.Title, &d.Content, &d.UpdatedAt, &d.CreatedAt, &pinned, &trashed, &private, &aliasesJSON, &d.Revision)
	if err != nil {
		return d, err
	}
	d.Pinned = pinned != 0
	d.Trashed = trashed != 0
	d.Private = private != 0
	if json.Unmarshal([]byte(aliasesJSON), &d.Aliases) != nil {
		d.Aliases = []string{}
	}
	return d, nil
}

func (s *server) document(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/documents/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "document id required", 400)
		return
	}
	switch r.Method {
	case "GET":
		d, err := s.documentByID(id)
		if err == sql.ErrNoRows {
			http.Error(w, "document not found", 404)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonOut(w, d)
	case "PUT":
		var incoming Doc
		if json.NewDecoder(r.Body).Decode(&incoming) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		aliasesJSON, _ := json.Marshal(incoming.Aliases)
		updated := time.Now().Format(time.RFC3339Nano)
		result, err := s.db.Exec(`UPDATE documents SET title=?,content=?,updated_at=?,pinned=?,trashed=?,private=?,aliases_json=?,revision=revision+1 WHERE id=? AND revision=?`,
			incoming.Title, incoming.Content, updated, incoming.Pinned, incoming.Trashed, incoming.Private, string(aliasesJSON), id, incoming.Revision)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			current, currentErr := s.documentByID(id)
			if currentErr == sql.ErrNoRows {
				http.Error(w, "document not found", 404)
				return
			}
			if currentErr != nil {
				http.Error(w, currentErr.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(current)
			return
		}
		current, err := s.documentByID(id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonOut(w, current)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

type searchHit struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	notebookID := r.URL.Query().Get("notebookId")
	if query == "" {
		jsonOut(w, []searchHit{})
		return
	}
	terms := strings.Fields(query)
	for i, term := range terms {
		terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"*`
	}
	expression := strings.Join(terms, " AND ")
	rows, err := s.db.Query(`SELECT d.id,d.title,
snippet(documents_fts,2,'<mark>','</mark>','…',18),d.updated_at
FROM documents_fts JOIN documents d ON d.id=documents_fts.document_id
WHERE documents_fts MATCH ? AND d.trashed=0 AND (?='' OR d.notebook_id=?)
ORDER BY bm25(documents_fts),d.updated_at DESC LIMIT 30`, expression, notebookID, notebookID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer rows.Close()
	hits := []searchHit{}
	for rows.Next() {
		var hit searchHit
		if err = rows.Scan(&hit.ID, &hit.Title, &hit.Snippet, &hit.UpdatedAt); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		hits = append(hits, hit)
	}
	jsonOut(w, hits)
}

func safeName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = regexp.MustCompile(`[^\p{L}\p{N}._-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" || s == "." || s == ".." {
		return "untitled"
	}
	return s
}
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	f, h, e := r.FormFile("file")
	if e != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer f.Close()
	name := fmt.Sprintf("%d-%s", time.Now().UnixNano(), safeName(h.Filename))
	os.MkdirAll("data/uploads", 0755)
	out, e := os.Create(filepath.Join("data/uploads", name))
	if e == nil {
		_, e = io.Copy(out, f)
		out.Close()
	}
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	jsonOut(w, map[string]string{"url": "/uploads/" + name})
}

func idFromPath(path string) string {
	s := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s = regexp.MustCompile(`[^a-zA-Z0-9\p{Han}]+`).ReplaceAllString(s, "-")
	if s == "" {
		s = fmt.Sprintf("doc-%d", time.Now().UnixNano())
	}
	return strings.ToLower(s)
}
func importedFile(fh *multipart.FileHeader) (string, []byte, error) {
	f, e := fh.Open()
	if e != nil {
		return "", nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 8<<20))
	return fh.Filename, b, e
}
func (s *server) importMD(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if e := r.ParseMultipartForm(64 << 20); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	bookID := r.FormValue("notebookId")
	books, e := s.load()
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	bi := -1
	for i := range books {
		if books[i].ID == bookID {
			bi = i
		}
	}
	if bi < 0 {
		http.Error(w, "notebook not found", 404)
		return
	}
	count := 0
	paths := r.MultipartForm.Value["paths"]
	for fileIndex, fh := range r.MultipartForm.File["files"] {
		path, b, e := importedFile(fh)
		if fileIndex < len(paths) && paths[fileIndex] != "" {
			path = paths[fileIndex]
		}
		if e != nil || strings.ToLower(filepath.Ext(path)) != ".md" {
			continue
		}
		parts := strings.Split(strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/"), "/")
		folders := &books[bi].Folders
		var target *Folder
		parentKey := bookID
		for _, p := range parts[:len(parts)-1] {
			id := parentKey + "-" + idFromPath(p)
			idx := -1
			for i := range *folders {
				if (*folders)[i].ID == id {
					idx = i
				}
			}
			if idx < 0 {
				*folders = append(*folders, Folder{ID: id, Title: p, Open: true, Docs: []Doc{}, Children: []Folder{}})
				idx = len(*folders) - 1
			}
			target = &(*folders)[idx]
			parentKey = target.ID
			folders = &target.Children
		}
		if target == nil {
			id := bookID + "-import"
			idx := -1
			for i := range books[bi].Folders {
				if books[bi].Folders[i].ID == id {
					idx = i
				}
			}
			if idx < 0 {
				books[bi].Folders = append(books[bi].Folders, Folder{ID: id, Title: "导入", Open: true, Docs: []Doc{}, Children: []Folder{}})
				idx = len(books[bi].Folders) - 1
			}
			target = &books[bi].Folders[idx]
		}
		title := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
		target.Docs = append(target.Docs, Doc{ID: fmt.Sprintf("%s-%d", idFromPath(title), time.Now().UnixNano()), Title: title, Content: string(b), UpdatedAt: "刚刚"})
		count++
	}
	if e = s.save(books); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	jsonOut(w, map[string]any{"ok": true, "imported": count})
}

func walkFolders(fs []Folder, prefix string, fn func(string, Doc)) {
	for _, f := range fs {
		p := filepath.Join(prefix, safeName(f.Title))
		for _, d := range f.Docs {
			fn(filepath.Join(p, safeName(d.Title)+".md"), d)
		}
		walkFolders(f.Children, p, fn)
	}
}
func (s *server) exportMD(w http.ResponseWriter, r *http.Request) {
	books, e := s.load()
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	id := r.URL.Query().Get("notebookId")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, b := range books {
		if id != "" && b.ID != id {
			continue
		}
		walkFolders(b.Folders, safeName(b.Title), func(path string, d Doc) { f, _ := zw.Create(path); io.WriteString(f, d.Content) })
	}
	zw.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="mdocman-export.zip"`)
	w.Write(buf.Bytes())
}

func (s *server) render(md string) template.HTML {
	var b bytes.Buffer
	s.md.Convert([]byte(prepareImageMetadataForRender(md)), &b)
	html := renderedImageSizePattern.ReplaceAllString(b.String(), ` width="$1" height="$2" style="width:$1px;height:$2px;max-width:100%;object-fit:contain"`)
	return template.HTML(html)
}

var imageMetadataPattern = regexp.MustCompile(`(!\[[^\]\n]*\]\([^\n]*?\))<!--\s*(\{[^}\n]*\})\s*-->`)
var renderedImageSizePattern = regexp.MustCompile(` title="mdocman-size-(\d+)x(\d+)"`)
var frontmatterOpenPattern = regexp.MustCompile(`^---[ \t]*\r?\n`)
var frontmatterClosePattern = regexp.MustCompile(`(?m)^---[ \t]*(?:\r?\n|$)`)

func markdownBody(source string) string {
	open := frontmatterOpenPattern.FindStringIndex(source)
	if open == nil {
		return source
	}
	rest := source[open[1]:]
	close := frontmatterClosePattern.FindStringIndex(rest)
	if close == nil {
		return source
	}
	return rest[close[1]:]
}

func prepareImageMetadataForRender(markdown string) string {
	return imageMetadataPattern.ReplaceAllStringFunc(markdown, func(source string) string {
		parts := imageMetadataPattern.FindStringSubmatch(source)
		if len(parts) != 3 {
			return source
		}
		var metadata struct {
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Href   string `json:"href"`
		}
		if json.Unmarshal([]byte(parts[2]), &metadata) != nil {
			return source
		}
		image := parts[1]
		if metadata.Width > 0 && metadata.Height > 0 {
			image = strings.TrimSuffix(image, ")") + fmt.Sprintf(` "mdocman-size-%dx%d")`, metadata.Width, metadata.Height)
		}
		if strings.TrimSpace(metadata.Href) != "" {
			href := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(strings.TrimSpace(metadata.Href))
			image = "[" + image + "](" + href + ")"
		}
		return image
	})
}
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9\p{Han}]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

const page = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{{.Title}}</title><link rel="stylesheet" href="/style.css"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css"><script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js"></script><script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/contrib/auto-render.min.js" onload="renderMathInElement(document.body)"></script><script type="module">import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';document.querySelectorAll('code.language-mermaid').forEach(e=>{const p=e.parentElement,d=document.createElement('div');d.className='mermaid';d.textContent=e.textContent;p.replaceWith(d)});mermaid.initialize({startOnLoad:true,theme:'neutral'});</script></head><body><header><a href="/">墨笺</a><nav>{{.Book}}</nav></header><div class="layout">{{if .Sidebar}}<aside><b>目录</b>{{range .Links}}<a {{if eq .URL $.Current}}class="active"{{end}} href="{{.URL}}">{{.Title}}</a>{{end}}</aside>{{end}}<main><article>{{.HTML}}</article></main></div><footer>由 Mdocman 增量生成 · {{.Date}}</footer></body></html>`
const css = `:root{color:#30332e;background:#faf9f5;font-family:Georgia,"Songti SC",serif}body{margin:0}header{height:64px;border-bottom:1px solid #e5e3dc;display:flex;align-items:center;justify-content:space-between;padding:0 max(24px,calc((100% - 1080px)/2));font-family:system-ui}a{color:#45604a;text-decoration:none}.layout{display:flex;max-width:1080px;margin:0 auto}.layout aside{width:220px;flex:none;padding:70px 24px;border-right:1px solid #e5e3dc;font:13px system-ui}.layout aside b{display:block;margin-bottom:14px}.layout aside a{display:block;padding:7px 9px;border-radius:5px}.layout aside a.active{background:#e4e9e2;font-weight:600}.layout main{width:100%;max-width:820px;margin:70px auto;padding:0 36px;min-height:70vh}article{font-size:18px;line-height:1.9}img{max-width:100%;border-radius:8px}table{border-collapse:collapse;width:100%;display:block;overflow:auto}th,td{border:1px solid #d8d8d2;padding:8px 12px}th{background:#efeee9}pre{background:#282b27;color:#e8e9e5;padding:18px;overflow:auto;border-radius:8px}code{font:14px ui-monospace,monospace}p code{background:#ecebe6;color:#7d4d3a;padding:2px 5px;border-radius:4px}blockquote{border-left:3px solid #78907c;background:#f0f3ed;margin:25px 0;padding:14px 20px}footer{text-align:center;color:#aaa;font:12px system-ui;padding:35px}@media(max-width:760px){.layout aside{display:none}.layout main{padding:0 22px}}`

const previewPage = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta http-equiv="Cache-Control" content="no-store"><title>{{.Title}} · 预览</title><style>{{.CSS}}</style></head><body><div class="layout"><main><article>{{.HTML}}</article></main></div><footer>由本地 Mdocman 服务实时渲染</footer></body></html>`

func (s *server) previewDocument(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/preview/")
	if id == "" {
		http.Error(w, "document id required", http.StatusBadRequest)
		return
	}
	var title, content, book string
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid preview", http.StatusBadRequest)
			return
		}
		title = strings.TrimSpace(r.FormValue("title"))
		content = r.FormValue("content")
		book = strings.TrimSpace(r.FormValue("book"))
	} else if r.Method == http.MethodGet {
		err := s.db.QueryRow(`SELECT d.title,d.content,n.title FROM documents d JOIN notebooks n ON n.id=d.notebook_id WHERE d.id=? AND d.trashed=0`, id).Scan(&title, &content, &book)
		if err == sql.ErrNoRows {
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if title == "" {
		title = "未命名笔记"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data: https:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
	t := template.Must(template.New("preview").Parse(previewPage))
	if err := t.Execute(w, map[string]any{"Title": title, "HTML": s.render(markdownBody(content)), "CSS": template.CSS(css)}); err != nil {
		log.Printf("preview render: %v", err)
	}
}

type buildManifest struct {
	Sidebar bool              `json:"sidebar"`
	Files   map[string]string `json:"files"`
}
type siteLink struct{ Title, URL string }

func randomToken() (string, error) {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *server) share(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var in struct {
		DocumentID string `json:"documentId"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.DocumentID == "" {
		http.Error(w, "documentId required", 400)
		return
	}
	var title, content, book, token string
	e := s.db.QueryRow(`SELECT d.title,d.content,n.title,COALESCE(sh.token,'') FROM documents d JOIN notebooks n ON n.id=d.notebook_id LEFT JOIN shares sh ON sh.document_id=d.id WHERE d.id=?`, in.DocumentID).Scan(&title, &content, &book, &token)
	if e == sql.ErrNoRows {
		http.Error(w, "document not found", 404)
		return
	}
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	if token == "" {
		for i := 0; i < 5; i++ {
			token, e = randomToken()
			if e != nil {
				break
			}
			_, e = s.db.Exec(`INSERT INTO shares(token,document_id,created_at) VALUES(?,?,?)`, token, in.DocumentID, time.Now().Format(time.RFC3339))
			if e == nil {
				break
			}
		}
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	dir := filepath.Join("public-site", "s", token)
	if e = os.MkdirAll(dir, 0755); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	os.WriteFile("public-site/style.css", []byte(css), 0644)
	f, e := os.Create(filepath.Join(dir, "index.html"))
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	defer f.Close()
	t := template.Must(template.New("share").Parse(page))
	e = t.Execute(f, map[string]any{"Title": title, "Book": book, "HTML": s.render(content), "Date": time.Now().Format("2006-01-02"), "Sidebar": false, "Links": []siteLink{}, "Current": "/s/" + token + "/"})
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	jsonOut(w, map[string]any{"ok": true, "id": token, "url": "/s/" + token + "/", "previewUrl": "/site/s/" + token + "/"})
}

func (s *server) build(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	books, e := s.load()
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	os.MkdirAll("public-site", 0755)
	os.WriteFile("public-site/style.css", []byte(css), 0644)
	options := struct {
		IncludeSidebar *bool `json:"includeSidebar"`
	}{}
	_ = json.NewDecoder(r.Body).Decode(&options)
	includeSidebar := true
	if options.IncludeSidebar != nil {
		includeSidebar = *options.IncludeSidebar
	}
	old := buildManifest{Files: map[string]string{}}
	if b, err := os.ReadFile("public-site/.mdocman-manifest.json"); err == nil {
		json.Unmarshal(b, &old)
	}
	next := buildManifest{Sidebar: includeSidebar, Files: map[string]string{}}
	t := template.Must(template.New("p").Parse(page))
	links := []siteLink{}
	for _, b := range books {
		walkFolders(b.Folders, "", func(_ string, d Doc) {
			links = append(links, siteLink{d.Title, "/" + slug(b.ID) + "/" + slug(d.ID) + "/"})
		})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Title < links[j].Title })
	navJSON, _ := json.Marshal(links)
	navHash := fmt.Sprintf("%x", sha256.Sum256(navJSON))
	changed := 0
	for _, b := range books {
		walkFolders(b.Folders, "", func(_ string, d Doc) {
			rel := filepath.Join(slug(b.ID), slug(d.ID), "index.html")
			sum := fmt.Sprintf("%x", sha256.Sum256([]byte(d.Title+d.Content+fmt.Sprint(includeSidebar)+navHash)))
			next.Files[rel] = sum
			if old.Files[rel] == sum {
				return
			}
			dir := filepath.Dir(filepath.Join("public-site", rel))
			os.MkdirAll(dir, 0755)
			f, _ := os.Create(filepath.Join("public-site", rel))
			current := "/" + slug(b.ID) + "/" + slug(d.ID) + "/"
			t.Execute(f, map[string]any{"Title": d.Title, "Book": b.Title, "HTML": s.render(d.Content), "Date": time.Now().Format("2006-01-02"), "Sidebar": includeSidebar, "Links": links, "Current": current})
			f.Close()
			changed++
		})
	}
	removed := 0
	for rel := range old.Files {
		if _, ok := next.Files[rel]; !ok {
			os.RemoveAll(filepath.Dir(filepath.Join("public-site", rel)))
			removed++
		}
	}
	var idx strings.Builder
	idx.WriteString(`<!doctype html><link rel="stylesheet" href="/style.css"><main><h1>我的笔记</h1><ul>`)
	for _, l := range links {
		fmt.Fprintf(&idx, `<li><a href="%s">%s</a></li>`, l.URL, template.HTMLEscapeString(l.Title))
	}
	idx.WriteString(`</ul></main>`)
	os.WriteFile("public-site/index.html", []byte(idx.String()), 0644)
	if b, e := os.ReadDir("data/uploads"); e == nil {
		os.MkdirAll("public-site/uploads", 0755)
		for _, x := range b {
			src, _ := os.Open(filepath.Join("data/uploads", x.Name()))
			dst, _ := os.Create(filepath.Join("public-site/uploads", x.Name()))
			io.Copy(dst, src)
			src.Close()
			dst.Close()
		}
	}
	manifest, _ := json.MarshalIndent(next, "", "  ")
	os.WriteFile("public-site/.mdocman-manifest.json", manifest, 0644)
	jsonOut(w, map[string]any{"ok": true, "output": "public-site", "changed": changed, "removed": removed, "sidebar": includeSidebar})
}

func migrateJSON(s *server) {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM notebooks`).Scan(&n)
	if n > 0 {
		return
	}
	b, e := os.ReadFile("data/notebooks.json")
	if e != nil {
		return
	}
	var books []Notebook
	if json.Unmarshal(b, &books) == nil {
		s.save(books)
	}
}
func main() {
	db, e := openDB()
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	s := &server{db: db, md: goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.Typographer), goldmark.WithParserOptions(parser.WithAutoHeadingID()))}
	migrateJSON(s)
	if cliRequested(s) {
		return
	}
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/api/notebooks", cors(s.notebooks))
	http.HandleFunc("/api/documents/", cors(s.document))
	http.HandleFunc("/api/search", cors(s.search))
	http.HandleFunc("/api/ai/providers", cors(s.aiProviders))
	http.HandleFunc("/api/ai/providers/", cors(s.aiProvider))
	http.HandleFunc("/api/ai/transform", cors(s.aiTransform))
	http.HandleFunc("/api/ai/chat", cors(s.aiChat))
	http.HandleFunc("/api/ai/conversations", cors(s.aiConversations))
	http.HandleFunc("/api/ai/conversations/", cors(s.aiConversation))
	http.HandleFunc("/api/templates", cors(s.templates))
	http.HandleFunc("/api/templates/", cors(s.template))
	http.HandleFunc("/api/capture/tokens", cors(s.captureTokens))
	http.HandleFunc("/api/capture/tokens/", cors(s.captureToken))
	http.HandleFunc("/api/capture", cors(s.capture))
	http.HandleFunc("/api/audio-memos", cors(s.audioMemos))
	http.HandleFunc("/api/audio-memos/", cors(s.audioMemo))
	http.HandleFunc("/api/sync", cors(s.syncSettings))
	http.HandleFunc("/api/sync/run", cors(s.syncRun))
	http.HandleFunc("/api/assets/describe", cors(s.describeAssets))
	http.HandleFunc("/api/preview/", cors(s.previewDocument))
	http.HandleFunc("/api/upload", cors(s.upload))
	http.HandleFunc("/api/import", cors(s.importMD))
	http.HandleFunc("/api/export", cors(s.exportMD))
	http.HandleFunc("/api/build", cors(s.build))
	http.HandleFunc("/api/share", cors(s.share))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("data/uploads"))))
	http.Handle("/audio/", http.StripPrefix("/audio/", http.FileServer(http.Dir("data/audio-memos"))))
	http.Handle("/site/", http.StripPrefix("/site/", http.FileServer(http.Dir("public-site"))))
	http.Handle("/s/", http.StripPrefix("/s/", http.FileServer(http.Dir("public-site/s"))))
	fmt.Printf("Mdocman API: http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
