package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseManagerCreatesSwitchesAndRemembersDatabase(t *testing.T) {
	directory := t.TempDir()
	manager, err := newDatabaseManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.activeName(); got != defaultDatabaseFile {
		t.Fatalf("initial database = %q, want %q", got, defaultDatabaseFile)
	}
	if _, err = manager.current().Exec(`INSERT INTO notebooks(id,title,description,accent,position) VALUES('default-note','默认库','','#123456',0)`); err != nil {
		t.Fatal(err)
	}
	if err = manager.createAndActivate("工作"); err != nil {
		t.Fatal(err)
	}
	if got := manager.activeName(); got != "工作.db" {
		t.Fatalf("active database = %q, want 工作.db", got)
	}
	catalog, err := manager.catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.KnowledgeBases) != 2 {
		t.Fatalf("knowledge base count = %d, want 2", len(catalog.KnowledgeBases))
	}
	var defaultRecords int
	if err = manager.current().QueryRow(`SELECT COUNT(*) FROM notebooks WHERE id='default-note'`).Scan(&defaultRecords); err != nil {
		t.Fatal(err)
	}
	if defaultRecords != 0 {
		t.Fatalf("new database contains %d records from the default database", defaultRecords)
	}
	if _, err = manager.current().Exec(`INSERT INTO notebooks(id,title,description,accent,position) VALUES('work-note','工作库','','#654321',0)`); err != nil {
		t.Fatal(err)
	}
	manager.close()

	reopened, err := newDatabaseManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	if got := reopened.activeName(); got != "工作.db" {
		t.Fatalf("remembered database = %q, want 工作.db", got)
	}
	var workRecords int
	if err = reopened.current().QueryRow(`SELECT COUNT(*) FROM notebooks WHERE id='work-note'`).Scan(&workRecords); err != nil {
		t.Fatal(err)
	}
	if workRecords != 1 {
		t.Fatalf("reopened work database contains %d work records, want 1", workRecords)
	}
}

func TestDefaultWorkspaceDirectoryIsDotMdocInUserHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := defaultWorkspaceDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".mdoc"); directory != want {
		t.Fatalf("default workspace = %q, want %q", directory, want)
	}
}

func TestMigrateLegacyWorkspaceCopiesDatabaseAndAssets(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, "legacy-data")
	workspace := filepath.Join(base, ".mdoc")
	if err := os.MkdirAll(filepath.Join(legacy, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	legacyDatabase, err := openDBAt(filepath.Join(legacy, defaultDatabaseFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacyDatabase.Exec(`INSERT INTO notebooks(id,title,description,accent,position) VALUES('legacy','旧笔记','','#123456',0)`); err != nil {
		t.Fatal(err)
	}
	if err = legacyDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(legacy, "uploads", "cover.png"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}

	if err = migrateLegacyWorkspace(workspace, legacy); err != nil {
		t.Fatal(err)
	}
	manager, err := newDatabaseManager(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.close()
	var title string
	if err = manager.current().QueryRow(`SELECT title FROM notebooks WHERE id='legacy'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "旧笔记" {
		t.Fatalf("migrated title = %q, want 旧笔记", title)
	}
	if content, readErr := os.ReadFile(filepath.Join(workspace, "uploads", "cover.png")); readErr != nil || string(content) != "image" {
		t.Fatalf("migrated asset = %q, %v", content, readErr)
	}
}

func TestNormalizeDatabaseNameRejectsPaths(t *testing.T) {
	for _, name := range []string{"", "../notes.db", "/tmp/notes.db", ".hidden.db", "bad\nname.db"} {
		if _, err := normalizeDatabaseName(name); err == nil {
			t.Fatalf("normalizeDatabaseName(%q) unexpectedly succeeded", name)
		}
	}
	if got, err := normalizeDatabaseName("个人"); err != nil || got != "个人.db" {
		t.Fatalf("normalizeDatabaseName(个人) = %q, %v", got, err)
	}
}

func TestKnowledgeBaseCatalogEndpointCreatesSwitchesAndRecolors(t *testing.T) {
	manager, err := newDatabaseManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.close()
	server := &server{databases: manager}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/knowledge-bases", bytes.NewBufferString(`{"name":"项目"}`))
	createResponse := httptest.NewRecorder()
	server.knowledgeBaseCatalog(createResponse, createRequest)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/knowledge-bases = %d %q", createResponse.Code, createResponse.Body.String())
	}
	var catalog knowledgeBaseCatalogResponse
	if err = json.Unmarshal(createResponse.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Active != "项目.db" || len(catalog.KnowledgeBases) != 2 {
		t.Fatalf("created catalog = %#v", catalog)
	}

	colorRequest := httptest.NewRequest(http.MethodPatch, "/api/knowledge-bases", bytes.NewBufferString(`{"name":"项目","color":"#14b8a6"}`))
	colorResponse := httptest.NewRecorder()
	server.knowledgeBaseCatalog(colorResponse, colorRequest)
	if colorResponse.Code != http.StatusOK {
		t.Fatalf("PATCH /api/knowledge-bases = %d %q", colorResponse.Code, colorResponse.Body.String())
	}
	if got := manager.colors["项目.db"]; got != "#14b8a6" {
		t.Fatalf("knowledge base color = %q, want #14b8a6", got)
	}

	switchRequest := httptest.NewRequest(http.MethodPut, "/api/knowledge-bases", bytes.NewBufferString(`{"name":"mdocman.db"}`))
	switchResponse := httptest.NewRecorder()
	server.knowledgeBaseCatalog(switchResponse, switchRequest)
	if switchResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/knowledge-bases = %d %q", switchResponse.Code, switchResponse.Body.String())
	}
	if got := manager.activeName(); got != defaultDatabaseFile {
		t.Fatalf("switched database = %q, want %q", got, defaultDatabaseFile)
	}
}

func TestKnowledgeBaseColorSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	manager, err := newDatabaseManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.setKnowledgeBaseColor(defaultDatabaseFile, "#3b82f6"); err != nil {
		t.Fatal(err)
	}
	manager.close()

	reopened, err := newDatabaseManager(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	catalog, err := reopened.catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.KnowledgeBases) != 1 || catalog.KnowledgeBases[0].Color != "#3b82f6" {
		t.Fatalf("reopened catalog = %#v", catalog)
	}
}
