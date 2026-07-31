package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type providerRoundResult struct {
	Text      string
	ToolCalls []aiToolCall
	Assistant aiMessage
}

func streamProviderRound(
	ctx context.Context,
	config aiProviderConfig,
	apiKey string,
	messages []aiMessage,
	tools []aiToolDefinition,
	onDelta func(string),
) (providerRoundResult, error) {
	switch config.Provider {
	case "openai", "openrouter":
		return streamOpenAICompatibleRound(ctx, config, apiKey, messages, tools, onDelta)
	case "anthropic":
		return streamAnthropicRound(ctx, config, apiKey, messages, tools, onDelta)
	case "google":
		return streamGoogleRound(ctx, config, apiKey, messages, tools, onDelta)
	default:
		return providerRoundResult{}, errors.New("unsupported AI provider")
	}
}

func providerEndpoint(config aiProviderConfig, suffix string) string {
	base := config.BaseURL
	if base == "" {
		switch config.Provider {
		case "openrouter":
			base = "https://openrouter.ai/api/v1"
		case "openai":
			base = "https://api.openai.com/v1"
		case "anthropic":
			base = "https://api.anthropic.com"
		case "google":
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
	}
	return strings.TrimRight(base, "/") + suffix
}

func sendProviderRequest(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload any,
) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		failure, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return nil, fmt.Errorf("provider returned %d: %s", response.StatusCode, strings.TrimSpace(string(failure)))
	}
	return response, nil
}

func openAIMessages(messages []aiMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.Role == "user" && len(message.Attachments) > 0 {
			content := make([]map[string]any, 0, len(message.Attachments)+1)
			for _, attachment := range message.Attachments {
				content = append(content, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": attachment.DataURL},
				})
			}
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			item["content"] = content
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			item["tool_calls"] = calls
		}
		if message.Role == "tool" {
			item["tool_call_id"] = message.ToolCallID
		}
		result = append(result, item)
	}
	return result
}

func openAITools(tools []aiToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, definition := range tools {
		result = append(result, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        definition.Name,
				"description": definition.Description,
				"parameters":  definition.Parameters,
			},
		})
	}
	return result
}

func streamOpenAICompatibleRound(
	ctx context.Context,
	config aiProviderConfig,
	apiKey string,
	messages []aiMessage,
	tools []aiToolDefinition,
	onDelta func(string),
) (providerRoundResult, error) {
	payload := map[string]any{
		"model":    config.Model,
		"messages": openAIMessages(messages),
		"stream":   true,
	}
	if len(tools) > 0 {
		payload["tools"] = openAITools(tools)
		payload["tool_choice"] = "auto"
	}
	response, err := sendProviderRequest(
		ctx,
		providerEndpoint(config, "/chat/completions"),
		map[string]string{"Authorization": "Bearer " + apiKey},
		payload,
	)
	if err != nil {
		return providerRoundResult{}, fmt.Errorf("%s: %w", config.Label, err)
	}
	defer response.Body.Close()

	type callDelta struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	type streamEvent struct {
		Choices []struct {
			Delta struct {
				Content   string      `json:"content"`
				ToolCalls []callDelta `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	callParts := map[int]*aiToolCall{}
	var full strings.Builder
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		data := sseData(scanner.Text())
		if data == "" || data == "[DONE]" {
			continue
		}
		var event streamEvent
		if json.Unmarshal([]byte(data), &event) != nil || len(event.Choices) == 0 {
			continue
		}
		delta := event.Choices[0].Delta
		if delta.Content != "" {
			full.WriteString(delta.Content)
			onDelta(delta.Content)
		}
		for _, part := range delta.ToolCalls {
			call := callParts[part.Index]
			if call == nil {
				call = &aiToolCall{}
				callParts[part.Index] = call
			}
			if part.ID != "" {
				call.ID = part.ID
			}
			call.Name += part.Function.Name
			call.Arguments += part.Function.Arguments
		}
	}
	if err = scanner.Err(); err != nil {
		return providerRoundResult{}, err
	}
	indexes := make([]int, 0, len(callParts))
	for index := range callParts {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]aiToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := *callParts[index]
		if call.ID == "" {
			call.ID = fmt.Sprintf("tool-%d", index)
		}
		calls = append(calls, call)
	}
	text := full.String()
	return providerRoundResult{
		Text:      text,
		ToolCalls: calls,
		Assistant: aiMessage{Role: "assistant", Content: text, ToolCalls: calls},
	}, nil
}

func anthropicMessages(messages []aiMessage) (string, []map[string]any) {
	var system strings.Builder
	result := []map[string]any{}
	for _, message := range messages {
		if message.Role == "system" {
			if system.Len() > 0 {
				system.WriteString("\n")
			}
			system.WriteString(message.Content)
			continue
		}
		if message.Role == "tool" {
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": message.ToolCallID,
				"content":     message.Content,
			}
			if len(result) > 0 && result[len(result)-1]["role"] == "user" {
				if content, ok := result[len(result)-1]["content"].([]map[string]any); ok {
					result[len(result)-1]["content"] = append(content, block)
					continue
				}
			}
			result = append(result, map[string]any{"role": "user", "content": []map[string]any{block}})
			continue
		}
		if message.Role == "user" && len(message.Attachments) > 0 {
			content := make([]map[string]any, 0, len(message.Attachments)+1)
			for _, attachment := range message.Attachments {
				_, data, _ := strings.Cut(attachment.DataURL, ",")
				content = append(content, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": attachment.MediaType,
						"data":       data,
					},
				})
			}
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			result = append(result, map[string]any{"role": "user", "content": content})
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			content := []map[string]any{}
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				var input any = map[string]any{}
				_ = json.Unmarshal([]byte(call.Arguments), &input)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    call.ID,
					"name":  call.Name,
					"input": input,
				})
			}
			result = append(result, map[string]any{"role": "assistant", "content": content})
			continue
		}
		result = append(result, map[string]any{"role": message.Role, "content": message.Content})
	}
	return system.String(), result
}

func anthropicTools(tools []aiToolDefinition) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, definition := range tools {
		result = append(result, map[string]any{
			"name":         definition.Name,
			"description":  definition.Description,
			"input_schema": definition.Parameters,
		})
	}
	return result
}

func streamAnthropicRound(
	ctx context.Context,
	config aiProviderConfig,
	apiKey string,
	messages []aiMessage,
	tools []aiToolDefinition,
	onDelta func(string),
) (providerRoundResult, error) {
	system, providerMessages := anthropicMessages(messages)
	payload := map[string]any{
		"model":      config.Model,
		"system":     system,
		"messages":   providerMessages,
		"max_tokens": 4096,
		"stream":     true,
	}
	if len(tools) > 0 {
		payload["tools"] = anthropicTools(tools)
	}
	response, err := sendProviderRequest(
		ctx,
		providerEndpoint(config, "/v1/messages"),
		map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		},
		payload,
	)
	if err != nil {
		return providerRoundResult{}, fmt.Errorf("%s: %w", config.Label, err)
	}
	defer response.Body.Close()

	type streamEvent struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
			Text  string          `json:"text"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	callParts := map[int]*aiToolCall{}
	var full strings.Builder
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		data := sseData(scanner.Text())
		if data == "" {
			continue
		}
		var event streamEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		if event.Type == "content_block_start" {
			if event.ContentBlock.Type == "tool_use" {
				callParts[event.Index] = &aiToolCall{
					ID:        event.ContentBlock.ID,
					Name:      event.ContentBlock.Name,
					Arguments: "",
				}
			}
			if event.ContentBlock.Type == "text" && event.ContentBlock.Text != "" {
				full.WriteString(event.ContentBlock.Text)
				onDelta(event.ContentBlock.Text)
			}
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			full.WriteString(event.Delta.Text)
			onDelta(event.Delta.Text)
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "input_json_delta" {
			if call := callParts[event.Index]; call != nil {
				call.Arguments += event.Delta.PartialJSON
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return providerRoundResult{}, err
	}
	indexes := make([]int, 0, len(callParts))
	for index := range callParts {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]aiToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := *callParts[index]
		if call.Arguments == "" {
			call.Arguments = "{}"
		}
		calls = append(calls, call)
	}
	text := full.String()
	return providerRoundResult{
		Text:      text,
		ToolCalls: calls,
		Assistant: aiMessage{Role: "assistant", Content: text, ToolCalls: calls},
	}, nil
}

func googleMessages(messages []aiMessage) (string, []map[string]any) {
	var system strings.Builder
	result := []map[string]any{}
	toolNames := map[string]string{}
	for _, message := range messages {
		if message.Role == "system" {
			if system.Len() > 0 {
				system.WriteString("\n")
			}
			system.WriteString(message.Content)
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			parts := []map[string]any{}
			if message.Content != "" {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			for _, call := range message.ToolCalls {
				var arguments any = map[string]any{}
				_ = json.Unmarshal([]byte(call.Arguments), &arguments)
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": arguments}})
				toolNames[call.ID] = call.Name
			}
			result = append(result, map[string]any{"role": "model", "parts": parts})
			continue
		}
		if message.Role == "tool" {
			var output any
			if json.Unmarshal([]byte(message.Content), &output) != nil {
				output = map[string]any{"result": message.Content}
			}
			if _, ok := output.(map[string]any); !ok {
				output = map[string]any{"result": output}
			}
			part := map[string]any{
				"functionResponse": map[string]any{
					"name":     toolNames[message.ToolCallID],
					"response": output,
				},
			}
			if len(result) > 0 && result[len(result)-1]["role"] == "user" {
				if parts, ok := result[len(result)-1]["parts"].([]map[string]any); ok {
					result[len(result)-1]["parts"] = append(parts, part)
					continue
				}
			}
			result = append(result, map[string]any{"role": "user", "parts": []map[string]any{part}})
			continue
		}
		if message.Role == "user" && len(message.Attachments) > 0 {
			parts := make([]map[string]any, 0, len(message.Attachments)+1)
			for _, attachment := range message.Attachments {
				_, data, _ := strings.Cut(attachment.DataURL, ",")
				parts = append(parts, map[string]any{
					"inlineData": map[string]any{
						"mimeType": attachment.MediaType,
						"data":     data,
					},
				})
			}
			if message.Content != "" {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			result = append(result, map[string]any{"role": "user", "parts": parts})
			continue
		}
		role := message.Role
		if role == "assistant" {
			role = "model"
		}
		result = append(result, map[string]any{"role": role, "parts": []map[string]any{{"text": message.Content}}})
	}
	return system.String(), result
}

func googleTools(tools []aiToolDefinition) []map[string]any {
	declarations := make([]map[string]any, 0, len(tools))
	for _, definition := range tools {
		declarations = append(declarations, map[string]any{
			"name":        definition.Name,
			"description": definition.Description,
			"parameters":  definition.Parameters,
		})
	}
	return []map[string]any{{"functionDeclarations": declarations}}
}

func streamGoogleRound(
	ctx context.Context,
	config aiProviderConfig,
	apiKey string,
	messages []aiMessage,
	tools []aiToolDefinition,
	onDelta func(string),
) (providerRoundResult, error) {
	system, contents := googleMessages(messages)
	payload := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":          contents,
	}
	if len(tools) > 0 {
		payload["tools"] = googleTools(tools)
	}
	endpoint := providerEndpoint(config, "/models/"+url.PathEscape(config.Model)+":streamGenerateContent") +
		"?alt=sse&key=" + url.QueryEscape(apiKey)
	response, err := sendProviderRequest(ctx, endpoint, nil, payload)
	if err != nil {
		return providerRoundResult{}, fmt.Errorf("%s: %w", config.Label, err)
	}
	defer response.Body.Close()

	type streamEvent struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	var full strings.Builder
	calls := []aiToolCall{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		data := sseData(scanner.Text())
		if data == "" {
			continue
		}
		var event streamEvent
		if json.Unmarshal([]byte(data), &event) != nil || len(event.Candidates) == 0 {
			continue
		}
		for _, part := range event.Candidates[0].Content.Parts {
			if part.Text != "" {
				full.WriteString(part.Text)
				onDelta(part.Text)
			}
			if part.FunctionCall != nil {
				arguments, _ := json.Marshal(part.FunctionCall.Args)
				calls = append(calls, aiToolCall{
					ID:        fmt.Sprintf("google-tool-%d", len(calls)),
					Name:      part.FunctionCall.Name,
					Arguments: string(arguments),
				})
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return providerRoundResult{}, err
	}
	text := full.String()
	return providerRoundResult{
		Text:      text,
		ToolCalls: calls,
		Assistant: aiMessage{Role: "assistant", Content: text, ToolCalls: calls},
	}, nil
}

func sseData(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:"))
}
