package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type assetDescriptionOutcome struct {
	Eligible            int `json:"eligible"`
	Described           int `json:"described"`
	SkippedUpToDate     int `json:"skippedUpToDate"`
	SkippedPrivacy      int `json:"skippedPrivacy"`
	SkippedUnreferenced int `json:"skippedUnreferenced"`
	SkippedUserAuthored int `json:"skippedUserAuthored"`
	Refused             int `json:"refused"`
}

func eligibleAsset(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".pdf":
		return true
	default:
		return false
	}
}

func assetReferences(name string, content string) bool {
	return strings.Contains(content, "/uploads/"+name) || strings.Contains(content, "uploads/"+name)
}

func (s *server) classifyAsset(name string) (string, error) {
	rows, err := s.database().Query(`SELECT content,private FROM documents WHERE trashed=0`)
	if err != nil {
		return "skip-private", err
	}
	defer rows.Close()
	publicReferences := 0
	for rows.Next() {
		var content string
		var private int
		if rows.Scan(&content, &private) != nil {
			return "skip-private", nil
		}
		if !assetReferences(name, content) {
			continue
		}
		if private != 0 || frontmatterPrivate(content) {
			return "skip-private", nil
		}
		publicReferences++
	}
	if publicReferences == 0 {
		return "skip-unreferenced", nil
	}
	return "send", nil
}

func managedDescriptionHash(path string) (string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := string(content)
	if !strings.Contains(text, "reflectAsset: true") {
		return "", false
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "sourceHash:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "sourceHash:")), true
		}
	}
	return "", true
}

func postProviderJSON(endpoint string, headers map[string]string, payload any) ([]byte, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	result, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("provider rejected asset (%d): %s", response.StatusCode, strings.TrimSpace(string(result)))
	}
	return result, nil
}

func describeAssetProvider(config aiProviderConfig, apiKey, name, mediaType string, data []byte) (string, error) {
	instruction := "Describe this asset concisely in Markdown. Include visible or extracted text under a '## Extracted text' heading when present. Return only the description."
	isSVG := strings.HasSuffix(strings.ToLower(name), ".svg")
	isPDF := strings.HasSuffix(strings.ToLower(name), ".pdf")
	encoded := base64.StdEncoding.EncodeToString(data)
	switch config.Provider {
	case "openai", "openrouter":
		base := config.BaseURL
		if base == "" {
			if config.Provider == "openrouter" {
				base = "https://openrouter.ai/api/v1"
			} else {
				base = "https://api.openai.com/v1"
			}
		}
		if isPDF && config.Provider == "openai" {
			payload := map[string]any{"model": config.Model, "input": []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": instruction}, map[string]string{"type": "input_file", "filename": name, "file_data": "data:application/pdf;base64," + encoded}}}}}
			body, err := postProviderJSON(strings.TrimRight(base, "/")+"/responses", map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + apiKey}, payload)
			if err != nil {
				return "", err
			}
			var result struct {
				OutputText string `json:"output_text"`
			}
			_ = json.Unmarshal(body, &result)
			if result.OutputText == "" {
				var detailed struct {
					Output []struct {
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"output"`
				}
				_ = json.Unmarshal(body, &detailed)
				for _, output := range detailed.Output {
					for _, part := range output.Content {
						result.OutputText += part.Text
					}
				}
			}
			return strings.TrimSpace(result.OutputText), nil
		}
		content := []any{map[string]string{"type": "text", "text": instruction}}
		if isSVG {
			content = append(content, map[string]string{"type": "text", "text": string(data)})
		} else if isPDF {
			content = append(content, map[string]any{"type": "file", "file": map[string]string{"filename": name, "file_data": "data:application/pdf;base64," + encoded}})
		} else {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + mediaType + ";base64," + encoded}})
		}
		body, err := postProviderJSON(strings.TrimRight(base, "/")+"/chat/completions", map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + apiKey}, map[string]any{"model": config.Model, "messages": []any{map[string]any{"role": "user", "content": content}}})
		if err != nil {
			return "", err
		}
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		_ = json.Unmarshal(body, &result)
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("provider returned no asset description")
		}
		return strings.TrimSpace(result.Choices[0].Message.Content), nil
	case "anthropic":
		base := config.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		content := []any{map[string]string{"type": "text", "text": instruction}}
		if isSVG {
			content = append(content, map[string]string{"type": "text", "text": string(data)})
		} else {
			partType := "image"
			if isPDF {
				partType = "document"
			}
			content = append(content, map[string]any{"type": partType, "source": map[string]string{"type": "base64", "media_type": mediaType, "data": encoded}})
		}
		body, err := postProviderJSON(strings.TrimRight(base, "/")+"/v1/messages", map[string]string{"Content-Type": "application/json", "x-api-key": apiKey, "anthropic-version": "2023-06-01"}, map[string]any{"model": config.Model, "max_tokens": 2048, "messages": []any{map[string]any{"role": "user", "content": content}}})
		if err != nil {
			return "", err
		}
		var result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		_ = json.Unmarshal(body, &result)
		if len(result.Content) == 0 {
			return "", fmt.Errorf("provider returned no asset description")
		}
		return strings.TrimSpace(result.Content[0].Text), nil
	case "google":
		base := config.BaseURL
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
		parts := []any{map[string]string{"text": instruction}}
		if isSVG {
			parts = append(parts, map[string]string{"text": string(data)})
		} else {
			parts = append(parts, map[string]any{"inline_data": map[string]string{"mime_type": mediaType, "data": encoded}})
		}
		endpoint := strings.TrimRight(base, "/") + "/models/" + url.PathEscape(config.Model) + ":generateContent?key=" + url.QueryEscape(apiKey)
		body, err := postProviderJSON(endpoint, map[string]string{"Content-Type": "application/json"}, map[string]any{"contents": []any{map[string]any{"parts": parts}}})
		if err != nil {
			return "", err
		}
		var result struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		_ = json.Unmarshal(body, &result)
		if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
			return "", fmt.Errorf("provider returned no asset description")
		}
		return strings.TrimSpace(result.Candidates[0].Content.Parts[0].Text), nil
	}
	return "", fmt.Errorf("unsupported provider")
}

func (s *server) describeAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		ProviderID string   `json:"providerId"`
		Names      []string `json:"names"`
	}
	_ = json.NewDecoder(r.Body).Decode(&input)
	config, apiKey, err := s.configuredProvider(input.ProviderID)
	if err != nil {
		http.Error(w, "add an AI provider before describing assets", 400)
		return
	}
	entries, err := os.ReadDir(s.uploadsDir())
	if errorsIsNotExist(err) {
		jsonOut(w, assetDescriptionOutcome{})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	wanted := map[string]bool{}
	for _, name := range input.Names {
		wanted[filepath.Base(name)] = true
	}
	outcome := assetDescriptionOutcome{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !eligibleAsset(name) || strings.HasSuffix(name, ".reflect.md") || (len(wanted) > 0 && !wanted[name]) {
			continue
		}
		outcome.Eligible++
		verdict, classifyErr := s.classifyAsset(name)
		if classifyErr != nil || verdict == "skip-private" {
			outcome.SkippedPrivacy++
			continue
		}
		if verdict == "skip-unreferenced" {
			outcome.SkippedUnreferenced++
			continue
		}
		path := filepath.Join(s.uploadsDir(), name)
		data, readErr := os.ReadFile(path)
		if readErr != nil || len(data) > 20<<20 {
			outcome.Refused++
			continue
		}
		hashBytes := sha256.Sum256(data)
		hash := hex.EncodeToString(hashBytes[:])
		descriptionPath := path + ".reflect.md"
		if existing, statErr := os.Stat(descriptionPath); statErr == nil && existing.Mode().IsRegular() {
			existingHash, managed := managedDescriptionHash(descriptionPath)
			if !managed {
				outcome.SkippedUserAuthored++
				continue
			}
			if existingHash == hash {
				outcome.SkippedUpToDate++
				continue
			}
		}
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mediaType == "" {
			mediaType = http.DetectContentType(data)
		}
		description, describeErr := describeAssetProvider(config, apiKey, name, mediaType, data)
		if describeErr != nil {
			outcome.Refused++
			continue
		}
		body := fmt.Sprintf("---\nreflectAsset: true\nsource: %q\nsourceHash: %s\nsourceSize: %d\nprovider: %s\nmodel: %q\ngeneratedAt: %q\n---\n\n%s\n", "assets/uploads/"+name, hash, len(data), config.Provider, config.Model, time.Now().Format(time.RFC3339Nano), description)
		if os.WriteFile(descriptionPath, []byte(body), 0644) == nil {
			outcome.Described++
		}
	}
	jsonOut(w, outcome)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
