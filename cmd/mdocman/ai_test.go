package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestFetchAIModelsNormalizesProviderResponses(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		basePath     string
		requestPath  string
		responseBody string
		want         []aiModelOption
	}{
		{
			name:         "openai",
			provider:     "openai",
			basePath:     "/v1",
			requestPath:  "/v1/models",
			responseBody: `{"data":[{"id":"gpt-z"},{"id":"gpt-a"}]}`,
			want:         []aiModelOption{{ID: "gpt-a", Label: "gpt-a"}, {ID: "gpt-z", Label: "gpt-z"}},
		},
		{
			name:         "openrouter",
			provider:     "openrouter",
			basePath:     "/api/v1",
			requestPath:  "/api/v1/models",
			responseBody: `{"data":[{"id":"vendor/model","name":"Model name"}]}`,
			want:         []aiModelOption{{ID: "vendor/model", Label: "Model name"}},
		},
		{
			name:         "anthropic",
			provider:     "anthropic",
			requestPath:  "/v1/models",
			responseBody: `{"data":[{"id":"claude-sonnet","display_name":"Claude Sonnet"}]}`,
			want:         []aiModelOption{{ID: "claude-sonnet", Label: "Claude Sonnet"}},
		},
		{
			name:        "google",
			provider:    "google",
			basePath:    "/v1beta",
			requestPath: "/v1beta/models",
			responseBody: `{"models":[` +
				`{"name":"models/text-embedding","displayName":"Embedding","supportedGenerationMethods":["embedContent"]},` +
				`{"name":"models/gemini-pro","displayName":"Gemini Pro","supportedGenerationMethods":["generateContent"]}` +
				`]}`,
			want: []aiModelOption{{ID: "gemini-pro", Label: "Gemini Pro"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.requestPath {
					t.Errorf("request path = %q, want %q", r.URL.Path, test.requestPath)
				}
				if test.provider == "openai" || test.provider == "openrouter" {
					if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
						t.Errorf("Authorization = %q", got)
					}
				}
				if test.provider == "anthropic" {
					if got := r.Header.Get("x-api-key"); got != "test-key" {
						t.Errorf("x-api-key = %q", got)
					}
					if got := r.Header.Get("anthropic-version"); got == "" {
						t.Error("anthropic-version is missing")
					}
				}
				if test.provider == "google" && r.URL.Query().Get("key") != "test-key" {
					t.Errorf("Google API key = %q", r.URL.Query().Get("key"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.responseBody))
			}))
			defer server.Close()

			models, err := fetchAIModels(context.Background(), test.provider, "test-key", server.URL+test.basePath)
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != len(test.want) {
				t.Fatalf("models = %#v, want %#v", models, test.want)
			}
			for index := range test.want {
				if models[index] != test.want[index] {
					t.Errorf("models[%d] = %#v, want %#v", index, models[index], test.want[index])
				}
			}
		})
	}
}

func TestRenderSelectionPrompt(t *testing.T) {
	if got := renderSelectionPrompt("Rewrite {{selectedText}} exactly", "$1 text"); got != "Rewrite $1 text exactly" {
		t.Fatalf("placeholder substitution corrupted selection: %q", got)
	}
}
