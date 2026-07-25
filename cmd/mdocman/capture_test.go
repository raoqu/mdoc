package main

import (
	"strings"
	"testing"
)

func TestCaptureBlockUpsertDeduplicates(t *testing.T) {
	first := captureBlock("abc", "Example", "https://example.com", "selected", "note", "", "2026-07-24T00:00:00Z")
	content := upsertCaptureBlock("# Today\n", "abc", first)
	if !strings.Contains(content, "## [[Links]]") || strings.Count(content, "mdocman-capture:abc:start") != 1 {
		t.Fatalf("capture block not inserted correctly: %s", content)
	}
	second := captureBlock("abc", "Updated", "https://example.com", "selected", "note", "/uploads/capture.jpg", "2026-07-24T01:00:00Z")
	updated := upsertCaptureBlock(content, "abc", second)
	if strings.Count(updated, "mdocman-capture:abc:start") != 1 || !strings.Contains(updated, "Updated") || strings.Contains(updated, "[Example]") {
		t.Fatalf("capture block was duplicated instead of updated: %s", updated)
	}
}

func TestCaptureTokenDigestIsStableAndNonPlaintext(t *testing.T) {
	if tokenDigest("secret") != tokenDigest("secret") {
		t.Fatal("token digest is not stable")
	}
	if strings.Contains(tokenDigest("secret"), "secret") {
		t.Fatal("token digest contains the plaintext token")
	}
}
