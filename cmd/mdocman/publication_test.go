package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mdocman/internal/hugoengine"
	"time"
)

func TestPublicationSelectionFailsClosed(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	notes := []struct {
		id, content string
		private     bool
	}{
		{"selected", "---\npublish: true\ntitle: 公开文档\n---\nVISIBLE", false},
		{"unmarked", "UNMARKED_SECRET", false},
		{"disabled", "---\npublish: false\n---\nDISABLED_SECRET", false},
		{"string", "---\npublish: 'true'\n---\nSTRING_SECRET", false},
		{"private-db", "---\npublish: true\n---\nPRIVATE_DB_SECRET", true},
		{"private-yaml", "---\npublish: true\nprivate: true\n---\nPRIVATE_YAML_SECRET", false},
		{"private-string", "---\npublish: true\nprivate: 'true'\n---\nPRIVATE_STRING_SECRET", false},
		{"draft", "---\npublish: true\ndraft: true\n---\nDRAFT_SECRET", false},
		{"trash", "---\npublish: true\n---\nTRASH_SECRET", false},
		{"invalid", "---\npublish: true\nx: [\n---\nINVALID_SECRET", false},
		{"open-header", "---\npublish: true\nOPEN_HEADER_SECRET", false},
	}
	for _, note := range notes {
		putChatToolDocument(t, s, note.id, note.id, note.content, note.private)
	}
	if _, err := s.db.Exec("UPDATE documents SET trashed=1 WHERE id='trash'"); err != nil {
		t.Fatal(err)
	}
	docs, err := publicationDocuments(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "selected" || docs[0].Title != "公开文档" {
		t.Fatalf("unexpected selection: %+v", docs)
	}
	// The marker must survive existing notebook save/load, export and backup paths.
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	after, err := publicationDocuments(s.db)
	if err != nil || len(after) != 1 {
		t.Fatalf("marker lost on save: %v %+v", err, after)
	}
}

func TestPublicationProjectionOnlyCopiesReferencedAssets(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	if err := os.MkdirAll(s.uploadsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"public.txt": "PUBLIC_ASSET", "secret.txt": "PRIVATE_ASSET"} {
		if err := os.WriteFile(filepath.Join(s.uploadsDir(), name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	putChatToolDocument(t, s, "public", "Public", "---\npublish: true\naliases: [/secret-route/]\nurl: /hijack/\nprivateField: PRIVATE_METADATA\n---\n[Download](/uploads/public.txt)\n[[Private]]\n[[Other|公开链接]]\n`[[Other]]`", false)
	putChatToolDocument(t, s, "other", "Other", "---\npublish: true\n---\nOTHER_PUBLIC", false)
	putChatToolDocument(t, s, "private", "Private", "PRIVATE_BODY [secret](/uploads/secret.txt)", true)
	docs, err := publicationDocuments(s.db)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err = s.exportPublication(source, docs); err != nil {
		t.Fatal(err)
	}
	projected := readPublicationTree(t, source)
	for _, secret := range []string{"PRIVATE_ASSET", "PRIVATE_BODY", "PRIVATE_METADATA", "/hijack/", "/secret-route/"} {
		if strings.Contains(projected, secret) {
			t.Fatalf("projection leaked %s", secret)
		}
	}
	for _, expected := range []string{"PUBLIC_ASSET", "attachments/uploads/public.txt", "relref", "`[[Other]]`"} {
		if !strings.Contains(projected, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	if _, err = s.publicationAsset("/uploads/%2e%2e%2fsecret.txt", t.TempDir()); err == nil {
		t.Fatal("accepted traversal")
	}
	if err = os.Symlink(filepath.Join(s.uploadsDir(), "secret.txt"), filepath.Join(s.uploadsDir(), "linked.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.publicationAsset("/uploads/linked.txt", t.TempDir()); err == nil {
		t.Fatal("copied symlink")
	}
	if _, err = s.publicationAsset("/uploads/missing.txt", t.TempDir()); err == nil {
		t.Fatal("silently dropped missing asset")
	}
}

func readPublicationTree(t *testing.T, root string) string {
	t.Helper()
	var result strings.Builder
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		result.WriteString(string(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.String()
}

func TestPublicationSettingsAndIsolation(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	config := defaultPublicationConfig()
	config.Title = "Saved title"
	config.DeployTarget = "deploy@192.0.2.10:/var/www/first/"
	payload, _ := json.Marshal(config)
	response := httptest.NewRecorder()
	s.publicationSettings(response, httptest.NewRequest(http.MethodPut, "/api/publication", strings.NewReader(string(payload))))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	_, root, err := s.publicationScope()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := readPublicationConfig(root)
	if err != nil || saved.Title != config.Title || saved.DeployTarget != config.DeployTarget {
		t.Fatalf("save: %v %+v", err, saved)
	}
	other, err := openDBAt(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s.db = other
	_, otherRoot, err := s.publicationScope()
	if err != nil || root == otherRoot {
		t.Fatalf("databases share publication directory: %v", err)
	}
	otherConfig := defaultPublicationConfig()
	otherConfig.DeployTarget = "deploy@192.0.2.20:/var/www/second/"
	otherPayload, _ := json.Marshal(otherConfig)
	response = httptest.NewRecorder()
	s.publicationSettings(response, httptest.NewRequest(http.MethodPut, "/api/publication", strings.NewReader(string(otherPayload))))
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	otherSaved, err := readPublicationConfig(otherRoot)
	if err != nil || otherSaved.DeployTarget != otherConfig.DeployTarget {
		t.Fatalf("second knowledge base save: %v %+v", err, otherSaved)
	}
	staleSave := httptest.NewRequest(http.MethodPut, "/api/publication", strings.NewReader(string(payload)))
	staleSave.Header.Set("X-Mdoc-Publication-Directory", root)
	response = httptest.NewRecorder()
	s.publicationSettings(response, staleSave)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale settings save accepted: %d", response.Code)
	}
	otherSaved, err = readPublicationConfig(otherRoot)
	if err != nil || otherSaved.DeployTarget != otherConfig.DeployTarget {
		t.Fatalf("stale save overwrote second knowledge base: %v %+v", err, otherSaved)
	}
	saved, err = readPublicationConfig(root)
	if err != nil || saved.DeployTarget != config.DeployTarget {
		t.Fatalf("first knowledge base overwritten: %v %+v", err, saved)
	}
	switched := httptest.NewRequest(http.MethodPost, "/api/publication/deploy", nil)
	switched.Header.Set("X-Mdoc-Publication-Directory", root)
	switchedResponse := httptest.NewRecorder()
	s.publicationActions(switchedResponse, switched)
	if switchedResponse.Code != http.StatusConflict {
		t.Fatalf("cross-database request accepted: %d", switchedResponse.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/publication/deploy", nil)
	request.Header.Set("Origin", "https://unrelated.example")
	response = httptest.NewRecorder()
	s.publicationActions(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin deploy: %d", response.Code)
	}
	response = httptest.NewRecorder()
	s.publicationActions(response, httptest.NewRequest(http.MethodGet, "/api/publication/deploy", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET deploy: %d", response.Code)
	}
	for _, target := range []string{"root@host:/", "root@host:/www/wwwroot", "root@host:/www/wwwroot/../", "root@host:/www/wwwroot/mdoc;touch-x", "-e evil:/www/wwwroot/mdoc"} {
		if validateDeployTarget(target) == nil {
			t.Fatalf("accepted %s", target)
		}
	}
	unlock, err := s.lockPublication()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err = s.lockPublication(); err == nil {
		t.Fatal("overlapping process accepted")
	}
}

func TestPublicationHugoIntegration(t *testing.T) {
	if os.Getenv("MDOC_TEST_THEME_INSTALL") != "1" {
		t.Skip("set MDOC_TEST_THEME_INSTALL=1 to exercise Stack module installation")
	}
	if !hugoengine.Extended() {
		t.Fatal("Stack integration tests require go test -tags extended")
	}
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	config := defaultPublicationConfig()
	config.BaseURL = "https://example.test/docs/"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := s.installPublicationTheme(ctx, config); err != nil {
		t.Fatal(err)
	}
	// From this point, neither Hugo, Go nor a Sass executable is available.
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(s.uploadsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.uploadsDir(), "public.txt"), []byte("PUBLIC_ATTACHMENT_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	putChatToolDocument(t, s, "public", "公开示例", "---\npublish: true\ntags: [示例]\n---\nPUBLIC_BODY_SENTINEL\n\n[附件](/uploads/public.txt)\n\n[[other|另一篇]]", false)
	putChatToolDocument(t, s, "other", "Other", "---\npublish: true\n---\nOTHER_SENTINEL", false)
	putChatToolDocument(t, s, "secret", "SECRET_TITLE_SENTINEL", "SECRET_BODY_SENTINEL", false)
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range books[0].Folders[0].Docs {
		books[0].Folders[0].Docs[i].Pinned = true
	}
	books[0].PinnedTree = []documentTreeNode{{ID: "secret", Children: []documentTreeNode{{ID: "other"}}}, {ID: "public"}}
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	scoped, root, err := s.publicationScope()
	if err != nil {
		t.Fatal(err)
	}
	result, err := scoped.runPublication(ctx, root, config, false)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := scoped.renderHugoPreview(ctx, config, pathPreviewPageData{Title: "Stack preview", Markdown: "# STACK_PREVIEW_SENTINEL\n\n![local](assets/local.png)\n\n```mermaid\ngraph LR\n A --> B\n```", TreeID: "stack", Tree: []pathPreviewTreeNode{{Name: "Stack tree", URL: "/note.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"STACK_PREVIEW_SENTINEL", "path-tree", "Stack tree", "mdoc/tree.css", "language-mermaid", `src="assets/local.png"`} {
		if !strings.Contains(string(rendered), expected) {
			t.Fatalf("Stack preview missing %s", expected)
		}
	}
	directoryPreview, err := scoped.renderHugoPreview(ctx, config, pathPreviewPageData{Title: "Directory", Directory: true, Entries: []pathPreviewEntry{{Name: "Start", URL: "start.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(directoryPreview), `href="start.md"`) {
		t.Fatal("Stack directory listing missing local links")
	}
	if !strings.Contains(hugoPreviewPolicy(rendered), "'sha256-") {
		t.Fatal("Stack inline initialization blocked by CSP")
	}
	if result.Documents != 2 {
		t.Fatalf("documents = %d", result.Documents)
	}
	localPreview := httptest.NewRecorder()
	scoped.knowledgePreview(localPreview, httptest.NewRequest(http.MethodGet, knowledgePreviewURL("other", "book", filepath.Base(root)), nil))
	if localPreview.Code != 200 {
		t.Fatal(localPreview.Body.String())
	}
	if got := renderedKnowledgeTree(t, localPreview.Body.String()); !reflect.DeepEqual(got, books[0].PinnedTree) {
		t.Fatalf("Stack local preview lost saved hierarchy: %+v", got)
	}
	if !strings.Contains(localPreview.Body.String(), "OTHER_SENTINEL") {
		t.Fatal("Stack local preview did not render requested document")
	}
	public := readPublicationTree(t, result.Output)
	preview := readPublicationTree(t, filepath.Join(root, "current", "preview"))
	if strings.Contains(public, "SECRET_") || strings.Contains(preview, "SECRET_") {
		t.Fatal("unmarked content leaked into site, search, or feed")
	}
	for _, expected := range []string{"PUBLIC_BODY_SENTINEL", "PUBLIC_ATTACHMENT_SENTINEL", "/docs/post/", "attachments/uploads/public.txt"} {
		if !strings.Contains(public, expected) {
			t.Fatalf("public site missing %s", expected)
		}
	}
	if !strings.Contains(preview, "/site/post/") {
		t.Fatal("preview links lost /site prefix")
	}
	if _, err = s.db.Exec("UPDATE documents SET content='withdrawn' WHERE id='public'"); err != nil {
		t.Fatal(err)
	}
	result, err = scoped.runPublication(ctx, root, config, false)
	if err != nil {
		t.Fatal(err)
	}
	next := readPublicationTree(t, result.Output)
	if strings.Contains(next, "PUBLIC_BODY_SENTINEL") || strings.Contains(next, "PUBLIC_ATTACHMENT_SENTINEL") {
		t.Fatal("withdrawn content or attachment retained")
	}
	// A failed build must preserve the successful output and never run rsync.
	fakeBin := t.TempDir()
	if err = os.MkdirAll(filepath.Join(root, "overrides", "layouts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "overrides", "layouts", "index.html"), []byte("{{ invalidTemplateFunction }}"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fakeBin, "rsync-called")
	if err = os.WriteFile(filepath.Join(fakeBin, "rsync"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err = scoped.runPublication(ctx, root, config, true); err == nil {
		t.Fatal("invalid Hugo template reported success")
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("rsync ran after Hugo failure")
	}
	if got := readPublicationTree(t, result.Output); got != next {
		t.Fatal("failed build modified previous output")
	}
}
