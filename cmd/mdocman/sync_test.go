package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMergeManifestSidesUnionsDocuments(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".mdocman"), 0755); err != nil {
		t.Fatal(err)
	}
	left := `{"version":1,"notebookId":"book","documents":[{"id":"a","title":"A","folderId":"f","kind":"note"}]}`
	right := `{"version":1,"notebookId":"book","documents":[{"id":"b","title":"B","folderId":"f","kind":"note"}]}`
	if err := mergeManifestSides(repo, left, right); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(repo, ".mdocman", "manifest.json"))
	var manifest syncManifest
	if json.Unmarshal(content, &manifest) != nil || len(manifest.Documents) != 2 {
		t.Fatalf("merged manifest = %s", content)
	}
}

func TestRunGitInitializesIsolatedRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := t.TempDir()
	if _, err := runGit(context.Background(), repo, "", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Fatal("git repository was not initialized")
	}
}
