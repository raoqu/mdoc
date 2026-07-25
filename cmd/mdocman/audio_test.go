package main

import (
	"strings"
	"testing"
)

func TestAudioExtension(t *testing.T) {
	if got := audioExtension("audio/webm;codecs=opus"); got != "webm" {
		t.Fatalf("audioExtension() = %q", got)
	}
	if got := audioExtension("audio/mp4"); got != "m4a" {
		t.Fatalf("audioExtension() = %q", got)
	}
}

func TestAppendAudioBacklinkIsIdempotent(t *testing.T) {
	content := appendAudioBacklink("# Today\n", "memo-id", "Audio memo 09:00", "memo.webm")
	if !strings.Contains(content, "## [[Audio memos]]") {
		t.Fatalf("missing audio heading: %s", content)
	}
	again := appendAudioBacklink(content, "memo-id", "Audio memo 09:00", "memo.webm")
	if again != content {
		t.Fatal("audio backlink was duplicated")
	}
}
