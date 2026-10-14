package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

const knowledgePreviewPrefix = "/api/knowledge-preview"

func knowledgePreviewURL(documentID, notebookID, scope string) string {
	query := url.Values{"notebookId": {notebookID}, "scope": {scope}}
	return knowledgePreviewPrefix + "/" + url.PathEscape(documentID) + "?" + query.Encode()
}

// Read the notebook tree directly, without public-site selection or output files.
func (s *server) knowledgePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost && !publicationSameOrigin(w, r) {
		return
	}
	s.publicationMu.RLock()
	defer s.publicationMu.RUnlock()
	scoped, root, err := s.publicationScope()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	scope := filepath.Base(root)
	if expected := r.Header.Get("X-Mdoc-Publication-Directory"); expected != "" && expected != root {
		http.Error(w, "知识库已切换，请重新打开知识库预览", http.StatusConflict)
		return
	}
	if expected := r.URL.Query().Get("scope"); expected != "" && expected != scope {
		http.Error(w, "知识库已切换，请重新打开知识库预览", http.StatusConflict)
		return
	}
	notebookID := r.URL.Query().Get("notebookId")
	documentID := strings.Trim(strings.TrimPrefix(r.URL.Path, knowledgePreviewPrefix), "/")
	if r.Method == http.MethodPost {
		if documentID != "" {
			http.Error(w, "method not allowed", 405)
			return
		}
		var input struct {
			NotebookID string `json:"notebookId"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		notebookID = input.NotebookID
	}
	books, err := scoped.load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var book *Notebook
	for i := range books {
		if books[i].ID == notebookID {
			book = &books[i]
			break
		}
	}
	if book == nil && notebookID == "" && len(books) > 0 {
		book = &books[0]
	}
	if book == nil && notebookID != "" {
		http.Error(w, "notebook not found", 404)
		return
	}
	if book == nil {
		book = &Notebook{Title: "知识库预览"}
	}
	documents := map[string]Doc{}
	var order []string
	for _, doc := range notebookDocuments(*book) {
		if !doc.Trashed {
			documents[doc.ID] = doc
			order = append(order, doc.ID)
		}
	}
	if documentID == "" {
		if len(book.PinnedTree) > 0 {
			documentID = book.PinnedTree[0].ID
		} else if len(order) > 0 {
			documentID = order[0]
		}
	}
	doc, exists := documents[documentID]
	if documentID != "" && !exists {
		http.Error(w, "document not found", 404)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		jsonOut(w, publicationResult{OK: true, Documents: len(documents), PreviewURL: knowledgePreviewURL(documentID, book.ID, scope), Log: "已加载知识库文档树"})
		return
	}
	selected := map[string]pathPreviewTreeNode{}
	for _, item := range documents {
		selected[item.ID] = pathPreviewTreeNode{Name: item.Title, URL: knowledgePreviewURL(item.ID, book.ID, scope), TreeKey: item.ID, Active: item.ID == documentID}
	}
	data := pathPreviewPageData{
		Title: book.Title, Markdown: "# 还没有笔记\n\n在知识库中新建笔记后刷新预览。",
		TreeID: scope + ":" + book.ID, Tree: projectDocumentTree(book.PinnedTree, selected),
		HomeURL: knowledgePreviewURL("", book.ID, scope),
	}
	if exists {
		data.Title, data.Markdown = doc.Title, markdownBody(doc.Content)
	}
	config, err := readPublicationConfig(root)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeHugoPreview(w, r, scoped, config, data)
}
