package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func knowledgePreviewFixture(t *testing.T) (*server, string, []documentTreeNode) {
	t.Helper()
	s, closeServer := chatToolTestServer(t)
	t.Cleanup(closeServer)
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	books[0].Folders[0].Docs = []Doc{
		{ID: "lkt", Title: "LKT", Pinned: true, Content: "---\npublish: true\n---\n# LKT_PUBLISHED"},
		{ID: "leaf", Title: "子主题", Pinned: true, Private: true, Content: "# PRIVATE_LOCAL_BODY"},
		{ID: "welcome", Title: "欢迎使用墨笺", Pinned: true, Content: "# WELCOME_UNPUBLISHED"},
		{ID: "vps", Title: "VPS", Pinned: true, Content: "---\ndraft: true\npublish: false\n---\n# VPS_DRAFT_BODY"},
		{ID: "trash", Title: "废纸篓文档", Pinned: true, Trashed: true, Content: "TRASH_BODY"},
		{ID: "unfixed", Title: "未固定", Content: "# UNFIXED_BODY"},
	}
	tree := []documentTreeNode{{ID: "welcome", Children: []documentTreeNode{{ID: "vps", Children: []documentTreeNode{{ID: "leaf"}}}}}, {ID: "lkt"}}
	books[0].PinnedTree = tree
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	_, root, err := s.publicationScope()
	if err != nil {
		t.Fatal(err)
	}
	config := defaultPublicationConfig()
	config.Theme = "default"
	config.DeployTarget = ""
	if err = writePublicationJSON(filepath.Join(root, "config.json"), config); err != nil {
		t.Fatal(err)
	}
	return s, root, tree
}

// Read the rendered nested list, so hierarchy is checked beyond title presence.
func renderedKnowledgeTree(t *testing.T, markup string) []documentTreeNode {
	t.Helper()
	document, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var aside *xhtml.Node
	var find func(*xhtml.Node)
	find = func(node *xhtml.Node) {
		if aside != nil {
			return
		}
		if node.Data == "aside" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && attr.Val == "path-tree" {
					aside = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(document)
	if aside == nil {
		t.Fatal("preview has no knowledge tree")
	}
	var descendants func(*xhtml.Node, string) *xhtml.Node
	descendants = func(node *xhtml.Node, tag string) *xhtml.Node {
		if node.Data == tag {
			return node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := descendants(child, tag); found != nil {
				return found
			}
		}
		return nil
	}
	var read func(*xhtml.Node) []documentTreeNode
	read = func(list *xhtml.Node) []documentTreeNode {
		var nodes []documentTreeNode
		for item := list.FirstChild; item != nil; item = item.NextSibling {
			if item.Data != "li" {
				continue
			}
			anchor := descendants(item, "a")
			if anchor == nil {
				t.Fatal("knowledge node has no link")
			}
			var address *url.URL
			for _, attr := range anchor.Attr {
				if attr.Key == "href" {
					address, err = url.Parse(attr.Val)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if address == nil || !strings.HasPrefix(address.Path, knowledgePreviewPrefix+"/") {
				t.Fatalf("link escaped knowledge preview: %v", address)
			}
			node := documentTreeNode{ID: strings.TrimPrefix(address.Path, knowledgePreviewPrefix+"/")}
			for child := item.FirstChild; child != nil; child = child.NextSibling {
				if child.Data == "ul" {
					node.Children = read(child)
				}
			}
			nodes = append(nodes, node)
		}
		return nodes
	}
	return read(descendants(aside, "ul"))
}

func TestKnowledgePreviewKeepsSavedTreeAcrossUnpublishedDraftAndPrivateNotes(t *testing.T) {
	s, root, want := knowledgePreviewFixture(t)
	request := httptest.NewRequest(http.MethodPost, knowledgePreviewPrefix, strings.NewReader(`{"notebookId":"book"}`))
	request.Header.Set("X-Mdoc-Publication-Directory", root)
	response := httptest.NewRecorder()
	s.knowledgePreview(response, request)
	var result publicationResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal(response.Body.String())
	}
	if result.Documents != 5 || !strings.HasPrefix(result.PreviewURL, knowledgePreviewPrefix+"/welcome?") {
		t.Fatalf("wrong notebook entry: %+v", result)
	}
	for _, item := range []struct{ id, body string }{{"welcome", "WELCOME_UNPUBLISHED"}, {"vps", "VPS_DRAFT_BODY"}, {"leaf", "PRIVATE_LOCAL_BODY"}, {"lkt", "LKT_PUBLISHED"}} {
		response = httptest.NewRecorder()
		s.knowledgePreview(response, httptest.NewRequest(http.MethodGet, knowledgePreviewURL(item.id, "book", filepath.Base(root)), nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), item.body) {
			t.Fatalf("%s not readable: %s", item.id, response.Body.String())
		}
		if got := renderedKnowledgeTree(t, response.Body.String()); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s tree = %+v, want %+v", item.id, got, want)
		}
		if strings.Contains(response.Body.String(), "废纸篓文档") || strings.Contains(response.Body.String(), "TRASH_BODY") {
			t.Fatal("trash joined the preview")
		}
	}
	public, err := publicationDocuments(s.database())
	if err != nil || len(public) != 1 || public[0].ID != "lkt" {
		t.Fatalf("preview changed publication selection: %+v %v", public, err)
	}
	if _, err = os.Stat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatal("local preview wrote publication artifacts")
	}
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	want = []documentTreeNode{{ID: "lkt", Children: []documentTreeNode{{ID: "vps", Children: []documentTreeNode{{ID: "leaf"}}}}}, {ID: "welcome"}}
	books[0].PinnedTree = want
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.knowledgePreview(response, httptest.NewRequest(http.MethodGet, result.PreviewURL, nil))
	if got := renderedKnowledgeTree(t, response.Body.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("refresh did not pick up dragged tree: %+v", got)
	}
}

func TestKnowledgePreviewRejectsStaleScopesAndOtherNotebooks(t *testing.T) {
	s, root, _ := knowledgePreviewFixture(t)
	books, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	books = append(books, Notebook{ID: "other", Title: "Other", Folders: []Folder{{ID: "other-folder", Title: "Other", Docs: []Doc{{ID: "other-doc", Title: "Other doc", Pinned: true, Content: "OTHER_NOTEBOOK_BODY"}}}}})
	if err = s.save(books); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.knowledgePreview(response, httptest.NewRequest(http.MethodGet, knowledgePreviewURL("other-doc", "book", filepath.Base(root)), nil))
	if response.Code != 404 {
		t.Fatal("cross-notebook document accepted")
	}
	response = httptest.NewRecorder()
	s.knowledgePreview(response, httptest.NewRequest(http.MethodGet, knowledgePreviewURL("welcome", "book", "stale"), nil))
	if response.Code != 409 {
		t.Fatal("stale preview scope accepted")
	}
	request := httptest.NewRequest(http.MethodPost, knowledgePreviewPrefix, strings.NewReader(`{"notebookId":"book"}`))
	request.Header.Set("X-Mdoc-Publication-Directory", "wrong")
	response = httptest.NewRecorder()
	s.knowledgePreview(response, request)
	if response.Code != 409 {
		t.Fatal("preview opened after knowledge base switch")
	}
	request = httptest.NewRequest(http.MethodPost, knowledgePreviewPrefix, strings.NewReader(`{"notebookId":"other"}`))
	response = httptest.NewRecorder()
	s.knowledgePreview(response, request)
	var result publicationResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || !strings.Contains(result.PreviewURL, "/other-doc?") {
		t.Fatalf("wrong active notebook: %s", response.Body.String())
	}
	response = httptest.NewRecorder()
	s.knowledgePreview(response, httptest.NewRequest(http.MethodGet, result.PreviewURL, nil))
	if got := renderedKnowledgeTree(t, response.Body.String()); !reflect.DeepEqual(got, []documentTreeNode{{ID: "other-doc"}}) {
		t.Fatalf("other notebook tree leaked: %+v", got)
	}
}
