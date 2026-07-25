package main

import "testing"

func TestFrontmatterPrivate(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{"private true", "---\ntitle: Secret\nprivate: true\n---\nbody", true},
		{"quoted true", "---\nprivate: 'true'\n---\nbody", true},
		{"false", "---\nprivate: false\n---\nbody", false},
		{"body is not metadata", "# Note\n\nprivate: true", false},
		{"malformed header fails closed", "---\nprivate: true\nbody", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := frontmatterPrivate(test.source); got != test.want {
				t.Fatalf("frontmatterPrivate() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStripFrontmatter(t *testing.T) {
	source := "---\ntitle: Example\nprivate: false\n---\n# Body\n"
	if got := stripFrontmatter(source); got != "# Body\n" {
		t.Fatalf("stripFrontmatter() = %q", got)
	}
}

func TestProviderDelta(t *testing.T) {
	tests := []struct {
		provider string
		payload  string
		want     string
	}{
		{"openai", `{"choices":[{"delta":{"content":"hello"}}]}`, "hello"},
		{"anthropic", `{"type":"content_block_delta","delta":{"text":"world"}}`, "world"},
		{"google", `{"candidates":[{"content":{"parts":[{"text":"gemini"}]}}]}`, "gemini"},
	}
	for _, test := range tests {
		if got := providerDelta(test.provider, []byte(test.payload)); got != test.want {
			t.Fatalf("providerDelta(%s) = %q, want %q", test.provider, got, test.want)
		}
	}
}

func TestRenderSelectionPrompt(t *testing.T) {
	if got := renderSelectionPrompt("Rewrite {{selectedText}} exactly", "$1 text"); got != "Rewrite $1 text exactly" {
		t.Fatalf("placeholder substitution corrupted selection: %q", got)
	}
}
