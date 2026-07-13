package main

import (
	"archive/zip"
	"bytes"
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
	"crypto/rand"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	_ "modernc.org/sqlite"
)

type Doc struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt"`
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
CREATE INDEX IF NOT EXISTS idx_folder_parent ON folders(notebook_id,parent_id,position);CREATE INDEX IF NOT EXISTS idx_docs_folder ON documents(notebook_id,folder_id,position);`
	_, err = db.Exec(schema)
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
	docs, err := s.db.Query(`SELECT id,folder_id,title,content,updated_at FROM documents WHERE notebook_id=? ORDER BY position`, book)
	if err != nil {
		return nil, err
	}
	defer docs.Close()
	for docs.Next() {
		var d Doc
		var folder sql.NullString
		if err = docs.Scan(&d.ID, &folder, &d.Title, &d.Content, &d.UpdatedAt); err != nil {
			return nil, err
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
	shares:=[]shareRow{};if rows,e:=s.db.Query(`SELECT token,document_id,created_at FROM shares`);e==nil{for rows.Next(){var x shareRow;if rows.Scan(&x.token,&x.documentID,&x.createdAt)==nil{shares=append(shares,x)}};rows.Close()}
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
					if _, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at) VALUES(?,?,?,?,?,?,?)`, d.ID, b.ID, f.ID, d.Title, d.Content, j, updated); err != nil {
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
	for _,x:=range shares{_,_ = tx.Exec(`INSERT OR IGNORE INTO shares(token,document_id,created_at) VALUES(?,?,?)`,x.token,x.documentID,x.createdAt)}
	return tx.Commit()
}
func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,OPTIONS")
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
		jsonOut(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
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
	s.md.Convert([]byte(md), &b)
	return template.HTML(b.String())
}
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9\p{Han}]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

const page = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{{.Title}}</title><link rel="stylesheet" href="/style.css"><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css"><script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js"></script><script defer src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/contrib/auto-render.min.js" onload="renderMathInElement(document.body)"></script><script type="module">import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';document.querySelectorAll('code.language-mermaid').forEach(e=>{const p=e.parentElement,d=document.createElement('div');d.className='mermaid';d.textContent=e.textContent;p.replaceWith(d)});mermaid.initialize({startOnLoad:true,theme:'neutral'});</script></head><body><header><a href="/">墨笺</a><nav>{{.Book}}</nav></header><div class="layout">{{if .Sidebar}}<aside><b>目录</b>{{range .Links}}<a {{if eq .URL $.Current}}class="active"{{end}} href="{{.URL}}">{{.Title}}</a>{{end}}</aside>{{end}}<main><article>{{.HTML}}</article></main></div><footer>由 Mdocman 增量生成 · {{.Date}}</footer></body></html>`
const css = `:root{color:#30332e;background:#faf9f5;font-family:Georgia,"Songti SC",serif}body{margin:0}header{height:64px;border-bottom:1px solid #e5e3dc;display:flex;align-items:center;justify-content:space-between;padding:0 max(24px,calc((100% - 1080px)/2));font-family:system-ui}a{color:#45604a;text-decoration:none}.layout{display:flex;max-width:1080px;margin:0 auto}.layout aside{width:220px;flex:none;padding:70px 24px;border-right:1px solid #e5e3dc;font:13px system-ui}.layout aside b{display:block;margin-bottom:14px}.layout aside a{display:block;padding:7px 9px;border-radius:5px}.layout aside a.active{background:#e4e9e2;font-weight:600}.layout main{width:100%;max-width:820px;margin:70px auto;padding:0 36px;min-height:70vh}article{font-size:18px;line-height:1.9}img{max-width:100%;border-radius:8px}table{border-collapse:collapse;width:100%;display:block;overflow:auto}th,td{border:1px solid #d8d8d2;padding:8px 12px}th{background:#efeee9}pre{background:#282b27;color:#e8e9e5;padding:18px;overflow:auto;border-radius:8px}code{font:14px ui-monospace,monospace}p code{background:#ecebe6;color:#7d4d3a;padding:2px 5px;border-radius:4px}blockquote{border-left:3px solid #78907c;background:#f0f3ed;margin:25px 0;padding:14px 20px}footer{text-align:center;color:#aaa;font:12px system-ui;padding:35px}@media(max-width:760px){.layout aside{display:none}.layout main{padding:0 22px}}`

type buildManifest struct {
	Sidebar bool              `json:"sidebar"`
	Files   map[string]string `json:"files"`
}
type siteLink struct{ Title, URL string }

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
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/api/notebooks", cors(s.notebooks))
	http.HandleFunc("/api/upload", cors(s.upload))
	http.HandleFunc("/api/import", cors(s.importMD))
	http.HandleFunc("/api/export", cors(s.exportMD))
	http.HandleFunc("/api/build", cors(s.build))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("data/uploads"))))
	http.Handle("/site/", http.StripPrefix("/site/", http.FileServer(http.Dir("public-site"))))
	fmt.Printf("Mdocman API: http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
