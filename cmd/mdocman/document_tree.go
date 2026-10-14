package main

import (
	"database/sql"
	"encoding/json"
	"net/url"
)

// The navigation hierarchy is independent of document storage folders.
type documentTreeNode struct {
	ID       string             `json:"id"`
	Children []documentTreeNode `json:"children,omitempty"`
}

func notebookDocuments(book Notebook) []Doc {
	var docs []Doc
	var visit func([]Folder)
	visit = func(folders []Folder) {
		for _, folder := range folders {
			docs = append(docs, folder.Docs...)
			visit(folder.Children)
		}
	}
	visit(book.Folders)
	return docs
}

// Missing, trashed, or unpinned parents disappear in place, promoting children
// to their former level. Newly pinned documents append to the root in source order.
func normalizeDocumentTree(nodes []documentTreeNode, docs []Doc) []documentTreeNode {
	eligible := map[string]bool{}
	for _, doc := range docs {
		eligible[doc.ID] = doc.Pinned && !doc.Trashed
	}
	seen := map[string]bool{}
	var visit func([]documentTreeNode) []documentTreeNode
	visit = func(nodes []documentTreeNode) []documentTreeNode {
		out := []documentTreeNode{}
		for _, node := range nodes {
			keep := eligible[node.ID] && !seen[node.ID]
			if keep {
				seen[node.ID] = true
			}
			children := visit(node.Children)
			if len(children) == 0 {
				children = nil
			}
			if keep {
				out = append(out, documentTreeNode{ID: node.ID, Children: children})
			} else {
				out = append(out, children...)
			}
		}
		return out
	}
	out := visit(nodes)
	for _, doc := range docs {
		if eligible[doc.ID] && !seen[doc.ID] {
			out = append(out, documentTreeNode{ID: doc.ID})
			seen[doc.ID] = true
		}
	}
	return out
}

func repairDocumentTree(tx *sql.Tx, documentID string) error {
	var notebookID, raw string
	if err := tx.QueryRow(`SELECT notebook_id FROM documents WHERE id=?`, documentID).Scan(&notebookID); err != nil {
		return err
	}
	err := tx.QueryRow(`SELECT nodes_json FROM pinned_trees WHERE notebook_id=?`, notebookID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var nodes []documentTreeNode
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &nodes); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT id,pinned,trashed FROM documents WHERE notebook_id=? ORDER BY position,id`, notebookID)
	if err != nil {
		return err
	}
	var docs []Doc
	for rows.Next() {
		var doc Doc
		if err = rows.Scan(&doc.ID, &doc.Pinned, &doc.Trashed); err != nil {
			rows.Close()
			return err
		}
		docs = append(docs, doc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalizeDocumentTree(nodes, docs))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO pinned_trees(notebook_id,nodes_json) VALUES(?,?) ON CONFLICT(notebook_id) DO UPDATE SET nodes_json=excluded.nodes_json`, notebookID, string(data))
	return err
}

func projectDocumentTree(nodes []documentTreeNode, documents map[string]pathPreviewTreeNode) []pathPreviewTreeNode {
	out := []pathPreviewTreeNode{}
	for _, node := range nodes {
		children := projectDocumentTree(node.Children, documents)
		if doc, ok := documents[node.ID]; ok {
			doc.Children = children
			doc.IsDir = len(children) > 0
			out = append(out, doc)
		} else {
			out = append(out, children...)
		}
	}
	return out
}

func (s *server) publicationTree(docs []publicationDocument) ([]pathPreviewTreeNode, error) {
	books, err := s.load()
	if err != nil {
		return nil, err
	}
	selected := map[string]pathPreviewTreeNode{}
	for _, doc := range docs {
		selected[doc.ID] = pathPreviewTreeNode{Name: doc.Title, URL: doc.Route, TreeKey: doc.Route}
	}
	tree := []pathPreviewTreeNode{}
	for _, book := range books {
		tree = append(tree, projectDocumentTree(book.PinnedTree, selected)...)
	}
	return tree, nil
}

func (s *server) knowledgePreviewTree(active string) ([]pathPreviewTreeNode, error) {
	books, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, book := range books {
		docs := notebookDocuments(book)
		contains := false
		selected := map[string]pathPreviewTreeNode{}
		for _, doc := range docs {
			if doc.ID == active {
				contains = true
			}
			selected[doc.ID] = pathPreviewTreeNode{Name: doc.Title, URL: "/api/preview/" + url.PathEscape(doc.ID), TreeKey: doc.ID, Active: doc.ID == active}
		}
		if contains {
			return projectDocumentTree(book.PinnedTree, selected), nil
		}
	}
	return nil, nil
}
