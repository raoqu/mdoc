package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEligibleAsset(t *testing.T) {
	for _, name := range []string{"photo.PNG", "scan.pdf", "diagram.svg", "clip.webp"} {
		if !eligibleAsset(name) {
			t.Fatalf("expected %s to be eligible", name)
		}
	}
	for _, name := range []string{"notes.md", "audio.m4a", "archive.zip"} {
		if eligibleAsset(name) {
			t.Fatalf("expected %s to be refused", name)
		}
	}
}

func TestManagedDescriptionHash(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "asset.reflect.md")
	if err := os.WriteFile(managed, []byte("---\nreflectAsset: true\nsourceHash: abc123\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if hash, ok := managedDescriptionHash(managed); !ok || hash != "abc123" {
		t.Fatalf("managedDescriptionHash() = %q, %v", hash, ok)
	}
	userAuthored := filepath.Join(dir, "user.md")
	if err := os.WriteFile(userAuthored, []byte("my own description"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := managedDescriptionHash(userAuthored); ok {
		t.Fatal("user-authored file must not be considered managed")
	}
}

func TestAssetReferences(t *testing.T) {
	if !assetReferences("photo.png", "![photo](/uploads/photo.png)") {
		t.Fatal("absolute upload reference was not detected")
	}
	if assetReferences("photo.png", "![other](/uploads/other.png)") {
		t.Fatal("unrelated upload was incorrectly detected")
	}
}
