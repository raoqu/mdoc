package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func providerTestServer(t *testing.T, events string, inspect func(map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var payload map[string]any
		if err = json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		inspect(payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, events)
	}))
}

func TestOpenAICompatibleRoundStreamsTextAndToolCalls(t *testing.T) {
	server := providerTestServer(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"Let me check. \",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"search_\",\"arguments\":\"{\\\"query\\\":\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"notes\",\"arguments\":\"\\\"Atlas\\\"}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n",
		func(payload map[string]any) {
			if _, ok := payload["tools"]; !ok {
				t.Error("OpenAI request did not include tools")
			}
			if payload["tool_choice"] != "auto" {
				t.Errorf("tool_choice = %v", payload["tool_choice"])
			}
		},
	)
	defer server.Close()
	var streamed strings.Builder
	result, err := streamProviderRound(
		context.Background(),
		aiProviderConfig{Provider: "openai", Label: "OpenAI", Model: "test", BaseURL: server.URL},
		"key",
		[]aiMessage{{Role: "user", Content: "What is Atlas?"}},
		chatToolDefinitions(false),
		func(delta string) { streamed.WriteString(delta) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "Let me check. " {
		t.Fatalf("streamed text = %q", streamed.String())
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "search_notes" || result.ToolCalls[0].Arguments != `{"query":"Atlas"}` {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
}

func TestAnthropicRoundNormalizesToolUse(t *testing.T) {
	server := providerTestServer(t,
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"read_notes\",\"input\":{}}}\n\n"+
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"documentIds\\\":[\\\"note-1\\\"]}\"}}\n\n",
		func(payload map[string]any) {
			if _, ok := payload["tools"]; !ok {
				t.Error("Anthropic request did not include tools")
			}
		},
	)
	defer server.Close()
	result, err := streamProviderRound(
		context.Background(),
		aiProviderConfig{Provider: "anthropic", Label: "Anthropic", Model: "test", BaseURL: server.URL},
		"key",
		[]aiMessage{{Role: "system", Content: "grounded"}, {Role: "user", Content: "read it"}},
		chatToolDefinitions(false),
		func(string) {},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "tool-1" || result.ToolCalls[0].Arguments != `{"documentIds":["note-1"]}` {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
}

func TestGoogleRoundNormalizesFunctionCall(t *testing.T) {
	server := providerTestServer(t,
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"list_recent_notes\",\"args\":{\"limit\":3}}}]}}]}\n\n",
		func(payload map[string]any) {
			if _, ok := payload["tools"]; !ok {
				t.Error("Google request did not include tools")
			}
		},
	)
	defer server.Close()
	result, err := streamProviderRound(
		context.Background(),
		aiProviderConfig{Provider: "google", Label: "Google", Model: "test", BaseURL: server.URL},
		"key",
		[]aiMessage{{Role: "user", Content: "recent"}},
		chatToolDefinitions(false),
		func(string) {},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "list_recent_notes" {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
}

func TestProviderMessagesEncodeImageAttachments(t *testing.T) {
	attachment := chatAttachment{
		ID:        "image-1",
		Name:      "photo.png",
		MediaType: "image/png",
		DataURL:   "data:image/png;base64,iVBORw==",
	}
	messages := []aiMessage{{
		Role:        "user",
		Content:     "What is shown?",
		Attachments: []chatAttachment{attachment},
	}}

	openAI := openAIMessages(messages)
	if _, ok := openAI[0]["content"].([]map[string]any); !ok {
		t.Fatalf("OpenAI image content = %#v", openAI[0]["content"])
	}
	_, anthropic := anthropicMessages(messages)
	if _, ok := anthropic[0]["content"].([]map[string]any); !ok {
		t.Fatalf("Anthropic image content = %#v", anthropic[0]["content"])
	}
	_, google := googleMessages(messages)
	parts, ok := google[0]["parts"].([]map[string]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("Google image parts = %#v", google[0]["parts"])
	}
}
