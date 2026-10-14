package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDocumentTreePersistsAndPromotesChildren(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	book := &books[0]
	book.Folders[0].Docs = []Doc{{ID: "a", Title: "A", Pinned: true}, {ID: "b", Title: "B", Pinned: true}, {ID: "c", Title: "C", Pinned: true}, {ID: "d", Title: "D", Pinned: true}}
	book.PinnedTree = []documentTreeNode{{ID: "a", Children: []documentTreeNode{{ID: "b", Children: []documentTreeNode{{ID: "c"}}}}}, {ID: "d"}}
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.load()
	if err != nil || !reflect.DeepEqual(loaded[0].PinnedTree, book.PinnedTree) {
		t.Fatalf("tree roundtrip: %+v %v", loaded, err)
	}
	// Single-document API updates must repair the stored tree in the same transaction.
	doc, err := s.documentByID("a")
	if err != nil {
		t.Fatal(err)
	}
	doc.Trashed = true
	payload, _ := json.Marshal(doc)
	response := httptest.NewRecorder()
	s.document(response, httptest.NewRequest(http.MethodPut, "/api/documents/a", strings.NewReader(string(payload))))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	loaded, err = s.load()
	want := []documentTreeNode{{ID: "b", Children: []documentTreeNode{{ID: "c"}}}, {ID: "d"}}
	if err != nil || !reflect.DeepEqual(loaded[0].PinnedTree, want) {
		t.Fatalf("children not promoted: %+v %v", loaded[0].PinnedTree, err)
	}
	// Restoring a parent must not steal its former children back.
	doc, err = s.documentByID("a")
	if err != nil {
		t.Fatal(err)
	}
	doc.Trashed = false
	payload, _ = json.Marshal(doc)
	response = httptest.NewRecorder()
	s.document(response, httptest.NewRequest(http.MethodPut, "/api/documents/a", strings.NewReader(string(payload))))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	loaded, err = s.load()
	want = append(want, documentTreeNode{ID: "a"})
	if err != nil || !reflect.DeepEqual(loaded[0].PinnedTree, want) {
		t.Fatalf("restored node reparented children: %+v %v", loaded[0].PinnedTree, err)
	}
	// Structural saves cover permanent deletion as well.
	loaded[0].Folders[0].Docs = loaded[0].Folders[0].Docs[2:]
	if err = s.save(loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.load()
	if err != nil || len(loaded[0].PinnedTree) != 2 || loaded[0].PinnedTree[0].ID != "c" {
		t.Fatalf("permanent deletion lost descendant: %+v %v", loaded[0].PinnedTree, err)
	}
}

func TestKnowledgePreviewUsesThemeTreeAndPreservesDraftInput(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	putChatToolDocument(t, s, "note", "Saved title", "# SAVED_BODY", false)
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range books[0].Folders[0].Docs {
		books[0].Folders[0].Docs[i].Pinned = true
	}
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	_, root, err := s.publicationScope()
	if err != nil {
		t.Fatal(err)
	}
	config := defaultPublicationConfig()
	config.Theme = "dark"
	if err = writePublicationJSON(filepath.Join(root, "config.json"), config); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.previewDocument(response, httptest.NewRequest(http.MethodGet, "/api/preview/note", nil))
	for _, part := range []string{`data-preview-theme="dark"`, `href="/api/preview/note"`, `SAVED_BODY`} {
		if response.Code != 200 || !strings.Contains(response.Body.String(), part) {
			t.Fatalf("preview missing %s: %s", part, response.Body.String())
		}
	}
	form := url.Values{"title": {"Unsaved title"}, "content": {"# UNSAVED_BODY"}, "book": {"Research"}}
	request := httptest.NewRequest(http.MethodPost, "/api/preview/note?theme=sepia", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	s.previewDocument(response, request)
	for _, part := range []string{`data-preview-theme="sepia"`, `data-preview-input`, `<textarea name="content"># UNSAVED_BODY</textarea>`, `<h1 id="unsaved_body">UNSAVED_BODY</h1>`} {
		if response.Code != 200 || !strings.Contains(response.Body.String(), part) {
			t.Fatalf("draft preview missing %s: %s", part, response.Body.String())
		}
	}
	doc, err := s.documentByID("note")
	if err != nil || !strings.Contains(doc.Content, "SAVED_BODY") || strings.Contains(doc.Content, "UNSAVED_BODY") {
		t.Fatal("preview modified saved content")
	}
}

func TestPublicationNativeThemesShareTreeAndFilterParents(t *testing.T) {
	s, closeServer := chatToolTestServer(t)
	defer closeServer()
	for _, doc := range []struct {
		id, body string
		private  bool
	}{
		{"hidden", "HIDDEN_PARENT_SENTINEL", true},
		{"child", "---\npublish: true\n---\nPUBLIC_CHILD_SENTINEL", false},
		{"sibling", "---\npublish: true\n---\nPUBLIC_SIBLING_SENTINEL", false},
	} {
		putChatToolDocument(t, s, doc.id, doc.id, doc.body, doc.private)
	}
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range books[0].Folders[0].Docs {
		books[0].Folders[0].Docs[i].Pinned = true
	}
	books[0].PinnedTree = []documentTreeNode{{ID: "hidden", Children: []documentTreeNode{{ID: "child"}}}, {ID: "sibling"}}
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	// All native themes build through the same engine without Go/Hugo executables.
	t.Setenv("PATH", t.TempDir())
	for _, theme := range []string{"default", "dark", "sepia", "yahei"} {
		t.Run(theme, func(t *testing.T) {
			config := defaultPublicationConfig()
			config.Theme = theme
			config.TreeTheme = "compact"
			config.BaseURL = "https://example.test/docs/"
			result, err := s.runPublication(context.Background(), filepath.Join(t.TempDir(), "publication"), config, false)
			if err != nil {
				t.Fatal(err)
			}
			all := readPublicationTree(t, result.Output)
			if strings.Contains(all, "HIDDEN_PARENT_SENTINEL") || strings.Contains(all, "/api/preview/") {
				t.Fatal("navigation leaked non-public data")
			}
			index, err := os.ReadFile(filepath.Join(result.Output, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range []string{"path-tree", "tree-theme-compact", "/docs/post/", "child", "sibling"} {
				if !strings.Contains(string(index), part) {
					t.Fatalf("missing %s: %s", part, index)
				}
			}
			preview, err := s.renderHugoPreview(context.Background(), config, pathPreviewPageData{Title: "Preview", Markdown: "# Preview\n", Tree: []pathPreviewTreeNode{{Name: "Child", URL: "/child.md", Active: true}}, TreeID: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(preview), "tree-theme-compact") || !strings.Contains(string(preview), "data-preview-theme=\""+theme+"\"") {
				t.Fatal("preview ignored shared Hugo theme/tree")
			}
		})
	}
}
