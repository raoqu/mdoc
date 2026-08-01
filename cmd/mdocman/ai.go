package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zalando/go-keyring"
)

const aiKeychainService = "mdocman-ai"

type aiProviderConfig struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Model     string `json:"model"`
	KeyHint   string `json:"keyHint"`
	BaseURL   string `json:"baseUrl,omitempty"`
	IsDefault bool   `json:"isDefault"`
	CreatedAt string `json:"createdAt"`
}

type aiModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type aiMessage struct {
	Role        string           `json:"role"`
	Content     string           `json:"content"`
	Attachments []chatAttachment `json:"attachments,omitempty"`
	ToolCalls   []aiToolCall     `json:"toolCalls,omitempty"`
	ToolCallID  string           `json:"toolCallId,omitempty"`
}

type chatConversation struct {
	ID         string `json:"id"`
	NotebookID string `json:"notebookId"`
	Title      string `json:"title"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type chatMessage struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversationId"`
	Role           string             `json:"role"`
	Content        string             `json:"content"`
	CreatedAt      string             `json:"createdAt"`
	Attachments    []chatAttachment   `json:"attachments,omitempty"`
	Sources        []chatSource       `json:"sources,omitempty"`
	Tools          []chatToolActivity `json:"tools,omitempty"`
}

func supportedAIProvider(provider string) bool {
	switch provider {
	case "openai", "anthropic", "google", "openrouter":
		return true
	default:
		return false
	}
}

func keyHint(key string) string {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) <= 4 {
		return "••••"
	}
	return "••••" + trimmed[len(trimmed)-4:]
}

func (s *server) listAIProviders() ([]aiProviderConfig, error) {
	rows, err := s.database().Query(`SELECT id,provider,label,model,key_hint,base_url,is_default,created_at FROM ai_providers ORDER BY is_default DESC,created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []aiProviderConfig{}
	for rows.Next() {
		var item aiProviderConfig
		var isDefault int
		if err = rows.Scan(&item.ID, &item.Provider, &item.Label, &item.Model, &item.KeyHint, &item.BaseURL, &isDefault, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.IsDefault = isDefault != 0
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *server) aiProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.listAIProviders()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonOut(w, items)
	case http.MethodPost:
		var input struct {
			Provider    string `json:"provider"`
			Label       string `json:"label"`
			Model       string `json:"model"`
			APIKey      string `json:"apiKey"`
			BaseURL     string `json:"baseUrl"`
			MakeDefault bool   `json:"makeDefault"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		input.Provider = strings.TrimSpace(input.Provider)
		input.Model = strings.TrimSpace(input.Model)
		input.APIKey = strings.TrimSpace(input.APIKey)
		if !supportedAIProvider(input.Provider) || input.Model == "" || input.APIKey == "" {
			http.Error(w, "provider, model and API key are required", 400)
			return
		}
		if input.BaseURL != "" {
			parsed, err := url.ParseRequestURI(input.BaseURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				http.Error(w, "base URL must be an http(s) URL", 400)
				return
			}
		}
		id, err := randomToken()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if input.Label == "" {
			input.Label = input.Provider
		}
		if err = keyring.Set(aiKeychainService, id, input.APIKey); err != nil {
			http.Error(w, "could not save the API key in the OS keychain: "+err.Error(), 500)
			return
		}
		tx, err := s.database().Begin()
		if err != nil {
			_ = keyring.Delete(aiKeychainService, id)
			http.Error(w, err.Error(), 500)
			return
		}
		defer tx.Rollback()
		var count int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM ai_providers`).Scan(&count)
		makeDefault := input.MakeDefault || count == 0
		if makeDefault {
			_, _ = tx.Exec(`UPDATE ai_providers SET is_default=0`)
		}
		now := time.Now().Format(time.RFC3339Nano)
		_, err = tx.Exec(`INSERT INTO ai_providers(id,provider,label,model,key_hint,base_url,is_default,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, input.Provider, input.Label, input.Model, keyHint(input.APIKey), strings.TrimRight(input.BaseURL, "/"), makeDefault, now)
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			_ = keyring.Delete(aiKeychainService, id)
			http.Error(w, err.Error(), 500)
			return
		}
		items, _ := s.listAIProviders()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(items)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) aiModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		BaseURL  string `json:"baseUrl"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.Provider = strings.TrimSpace(input.Provider)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	if !supportedAIProvider(input.Provider) || input.APIKey == "" {
		http.Error(w, "provider and API key are required", http.StatusBadRequest)
		return
	}
	if input.BaseURL != "" {
		parsed, err := url.ParseRequestURI(input.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			http.Error(w, "base URL must be an http(s) URL", http.StatusBadRequest)
			return
		}
	}
	models, err := fetchAIModels(r.Context(), input.Provider, input.APIKey, input.BaseURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonOut(w, map[string]any{"models": models})
}

func fetchAIModels(ctx context.Context, provider, apiKey, baseURL string) ([]aiModelOption, error) {
	base := strings.TrimRight(baseURL, "/")
	endpoint := ""
	switch provider {
	case "openai":
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		endpoint = base + "/models"
	case "openrouter":
		if base == "" {
			base = "https://openrouter.ai/api/v1"
		}
		endpoint = base + "/models"
	case "anthropic":
		if base == "" {
			base = "https://api.anthropic.com"
		}
		endpoint = base + "/v1/models?limit=1000"
	case "google":
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
		endpoint = base + "/models?pageSize=1000&key=" + url.QueryEscape(apiKey)
	default:
		return nil, errors.New("unsupported AI provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if provider == "openai" || provider == "openrouter" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else if provider == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch model list: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		message := strings.TrimSpace(string(failure))
		if message == "" {
			message = response.Status
		}
		return nil, fmt.Errorf("model API returned %d: %s", response.StatusCode, message)
	}

	models := []aiModelOption{}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	if provider == "google" {
		var payload struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		if err = decoder.Decode(&payload); err != nil {
			return nil, fmt.Errorf("could not decode model list: %w", err)
		}
		for _, item := range payload.Models {
			if !containsString(item.SupportedGenerationMethods, "generateContent") {
				continue
			}
			id := strings.TrimPrefix(strings.TrimSpace(item.Name), "models/")
			models = append(models, aiModelOption{ID: id, Label: strings.TrimSpace(item.DisplayName)})
		}
	} else {
		var payload struct {
			Data []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
		}
		if err = decoder.Decode(&payload); err != nil {
			return nil, fmt.Errorf("could not decode model list: %w", err)
		}
		for _, item := range payload.Data {
			label := strings.TrimSpace(item.DisplayName)
			if label == "" {
				label = strings.TrimSpace(item.Name)
			}
			models = append(models, aiModelOption{ID: strings.TrimSpace(item.ID), Label: label})
		}
	}

	unique := make(map[string]aiModelOption, len(models))
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		if model.Label == "" {
			model.Label = model.ID
		}
		unique[model.ID] = model
	}
	models = models[:0]
	for _, model := range unique {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID)
	})
	return models, nil
}

func (s *server) aiProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/providers/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "provider id required", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input struct {
			MakeDefault bool `json:"makeDefault"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !input.MakeDefault {
			http.Error(w, "makeDefault must be true", 400)
			return
		}
		tx, err := s.database().Begin()
		if err == nil {
			_, err = tx.Exec(`UPDATE ai_providers SET is_default=0`)
		}
		if err == nil {
			var result sql.Result
			result, err = tx.Exec(`UPDATE ai_providers SET is_default=1 WHERE id=?`, id)
			if err == nil {
				if affected, _ := result.RowsAffected(); affected == 0 {
					err = sql.ErrNoRows
				}
			}
		}
		if err == nil {
			err = tx.Commit()
		} else if tx != nil {
			_ = tx.Rollback()
		}
		if err == sql.ErrNoRows {
			http.Error(w, "provider not found", 404)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		items, _ := s.listAIProviders()
		jsonOut(w, items)
	case http.MethodDelete:
		var wasDefault int
		err := s.database().QueryRow(`SELECT is_default FROM ai_providers WHERE id=?`, id).Scan(&wasDefault)
		if err == sql.ErrNoRows {
			http.Error(w, "provider not found", 404)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tx, err := s.database().Begin()
		if err == nil {
			_, err = tx.Exec(`DELETE FROM ai_providers WHERE id=?`, id)
		}
		if err == nil && wasDefault != 0 {
			_, err = tx.Exec(`UPDATE ai_providers SET is_default=1 WHERE id=(SELECT id FROM ai_providers ORDER BY created_at LIMIT 1)`)
		}
		if err == nil {
			err = tx.Commit()
		} else if tx != nil {
			_ = tx.Rollback()
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = keyring.Delete(aiKeychainService, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func frontmatterPrivate(source string) bool {
	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return false
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		// A leading but unterminated metadata block cannot be proven public.
		return true
	}
	for _, line := range strings.Split(normalized[4:4+end], "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "private") {
			return strings.EqualFold(strings.Trim(strings.TrimSpace(parts[1]), "'\""), "true")
		}
	}
	return false
}

func stripFrontmatter(source string) string {
	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return source
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return source
	}
	after := 4 + end + len("\n---")
	if after < len(normalized) && normalized[after] == '\n' {
		after++
	}
	return normalized[after:]
}

func (s *server) cloudSafeDocument(id string) (Doc, error) {
	doc, err := s.documentByID(id)
	if err != nil {
		return doc, err
	}
	if doc.Private || frontmatterPrivate(doc.Content) {
		return doc, errors.New("this note is private and cannot be sent to AI")
	}
	return doc, nil
}

func (s *server) configuredProvider(id string) (aiProviderConfig, string, error) {
	var config aiProviderConfig
	var isDefault int
	query := `SELECT id,provider,label,model,key_hint,base_url,is_default,created_at FROM ai_providers `
	var err error
	if id == "" {
		err = s.database().QueryRow(query+`ORDER BY is_default DESC,created_at LIMIT 1`).Scan(&config.ID, &config.Provider, &config.Label, &config.Model, &config.KeyHint, &config.BaseURL, &isDefault, &config.CreatedAt)
	} else {
		err = s.database().QueryRow(query+`WHERE id=?`, id).Scan(&config.ID, &config.Provider, &config.Label, &config.Model, &config.KeyHint, &config.BaseURL, &isDefault, &config.CreatedAt)
	}
	if err != nil {
		return config, "", err
	}
	config.IsDefault = isDefault != 0
	apiKey, err := keyring.Get(aiKeychainService, config.ID)
	if err != nil {
		return config, "", fmt.Errorf("API key unavailable in OS keychain: %w", err)
	}
	return config, apiKey, nil
}

func writeAIEvent(w http.ResponseWriter, flusher http.Flusher, event any) {
	payload, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func beginAIStream(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	return flusher, true
}

func (s *server) aiTransform(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		DocumentID   string `json:"documentId"`
		ProviderID   string `json:"providerId"`
		PromptBody   string `json:"promptBody"`
		SelectedText string `json:"selectedText"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.DocumentID == "" || strings.TrimSpace(input.SelectedText) == "" || strings.TrimSpace(input.PromptBody) == "" {
		http.Error(w, "document, prompt and selection are required", 400)
		return
	}
	doc, err := s.cloudSafeDocument(input.DocumentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if !strings.Contains(stripFrontmatter(doc.Content), input.SelectedText) {
		http.Error(w, "selection no longer belongs to the current note", http.StatusConflict)
		return
	}
	config, apiKey, err := s.configuredProvider(input.ProviderID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	flusher, ok := beginAIStream(w)
	if !ok {
		return
	}
	system := "You transform a text selection from the user's Markdown note. Reply with only the resulting Markdown text—no preamble, explanation, or wrapping code fence. Match the language of the selection unless the prompt says otherwise."
	prompt := renderSelectionPrompt(input.PromptBody, input.SelectedText)
	full, err := streamProvider(r, config, apiKey, []aiMessage{{Role: "system", Content: system}, {Role: "user", Content: prompt}}, func(delta string) {
		writeAIEvent(w, flusher, map[string]any{"type": "text-delta", "text": delta})
	})
	if err != nil {
		writeAIEvent(w, flusher, map[string]any{"type": "error", "message": err.Error()})
		return
	}
	writeAIEvent(w, flusher, map[string]any{"type": "complete", "text": full})
}

func renderSelectionPrompt(body, selection string) string {
	if strings.Contains(body, "{{selectedText}}") {
		return strings.ReplaceAll(body, "{{selectedText}}", selection)
	}
	return body + "\n\nUse the following text in triple quotes as context:\n\"\"\"\n" + selection + "\n\"\"\""
}

type groundedNote struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"-"`
}

func ftsExpression(query string) string {
	terms := strings.Fields(query)
	if len(terms) > 12 {
		terms = terms[:12]
	}
	for i, term := range terms {
		terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"*`
	}
	return strings.Join(terms, " OR ")
}

func (s *server) groundingNotes(notebookID, query string) ([]groundedNote, error) {
	expression := ftsExpression(query)
	var rows *sql.Rows
	var err error
	if expression == "" {
		rows, err = s.database().Query(`SELECT id,title,content FROM documents WHERE notebook_id=? AND trashed=0 AND private=0 ORDER BY updated_at DESC LIMIT 8`, notebookID)
	} else {
		rows, err = s.database().Query(`SELECT d.id,d.title,d.content FROM documents_fts JOIN documents d ON d.id=documents_fts.document_id WHERE documents_fts MATCH ? AND d.notebook_id=? AND d.trashed=0 AND d.private=0 ORDER BY bm25(documents_fts),d.updated_at DESC LIMIT 8`, expression, notebookID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []groundedNote{}
	budget := 32000
	for rows.Next() {
		var item groundedNote
		if err = rows.Scan(&item.ID, &item.Title, &item.Content); err != nil {
			return nil, err
		}
		if frontmatterPrivate(item.Content) {
			continue
		}
		item.Content = stripFrontmatter(item.Content)
		if len(item.Content) > 12000 {
			item.Content = item.Content[:12000] + "\n[…truncated]"
		}
		if len(item.Content) > budget {
			item.Content = item.Content[:budget]
		}
		budget -= len(item.Content)
		result = append(result, item)
		if budget <= 0 {
			break
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	assetPattern := regexp.MustCompile(`/uploads/([^\s)"']+)`)
	seenAssets := map[string]bool{}
	for index := range result {
		for _, match := range assetPattern.FindAllStringSubmatch(result[index].Content, -1) {
			if len(match) < 2 {
				continue
			}
			name := filepath.Base(match[1])
			if seenAssets[name] {
				continue
			}
			seenAssets[name] = true
			verdict, classifyErr := s.classifyAsset(name)
			if classifyErr != nil || verdict != "send" {
				continue
			}
			description, readErr := os.ReadFile(filepath.Join(s.uploadsDir(), name+".reflect.md"))
			if readErr == nil {
				body := stripFrontmatter(string(description))
				if len(body) > 6000 {
					body = body[:6000]
				}
				result[index].Content += "\n\n[Local description for attachment " + name + "]\n" + body
			}
		}
	}
	return result, nil
}

func chatSystemPrompt(today string, notes []groundedNote) string {
	var context strings.Builder
	for _, note := range notes {
		fmt.Fprintf(&context, "\n\n<note title=%q id=%q>\n%s\n</note>", note.Title, note.ID, note.Content)
	}
	return "You are Reflect's assistant embedded in the user's personal note graph. Today's date is " + today + ". Answer in concise Markdown. Ground answers only in the supplied non-private notes. If the notes do not cover the question, say so plainly. Cite every note you use with its exact wiki link, such as [[Project Atlas]]. Never invent note titles or private content.\n\nAvailable notes:" + context.String()
}

func titleForConversation(message string) string {
	title := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if len([]rune(title)) > 48 {
		title = string([]rune(title)[:48]) + "…"
	}
	if title == "" {
		return "新对话"
	}
	return title
}

func (s *server) aiChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var input struct {
		ConversationID string           `json:"conversationId"`
		NotebookID     string           `json:"notebookId"`
		ProviderID     string           `json:"providerId"`
		ModelID        string           `json:"modelId"`
		SystemPrompt   string           `json:"systemPrompt"`
		SemanticSearch bool             `json:"semanticSearchEnabled"`
		Message        string           `json:"message"`
		Attachments    []chatAttachment `json:"attachments"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.NotebookID == "" || (strings.TrimSpace(input.Message) == "" && len(input.Attachments) == 0) {
		http.Error(w, "notebook and a message or image are required", 400)
		return
	}
	validatedAttachments, err := validateChatAttachments(input.Attachments)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	input.Attachments = validatedAttachments
	config, apiKey, err := s.configuredProvider(input.ProviderID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	input.ModelID = strings.TrimSpace(input.ModelID)
	if input.ModelID != "" {
		if len(input.ModelID) > 200 || strings.ContainsAny(input.ModelID, "\r\n") {
			http.Error(w, "invalid model id", 400)
			return
		}
		config.Model = input.ModelID
	}
	conversationID := input.ConversationID
	now := time.Now().Format(time.RFC3339Nano)
	if conversationID == "" {
		conversationID, err = randomToken()
		if err == nil {
			title := input.Message
			if strings.TrimSpace(title) == "" && len(input.Attachments) > 0 {
				title = input.Attachments[0].Name
			}
			_, err = s.database().Exec(`INSERT INTO chat_conversations(id,notebook_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, conversationID, input.NotebookID, titleForConversation(title), now, now)
		}
	} else {
		var storedNotebook string
		err = s.database().QueryRow(`SELECT notebook_id FROM chat_conversations WHERE id=?`, conversationID).Scan(&storedNotebook)
		if err == nil && storedNotebook != input.NotebookID {
			err = errors.New("conversation belongs to another notebook")
		}
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	context, err := s.loadChatGraphContext(input.NotebookID)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	semanticSearchActive := input.SemanticSearch && s.semanticRuntime().readyFor(s.database())
	messages := []aiMessage{{
		Role:    "system",
		Content: toolChatSystemPrompt(time.Now().Format("2006-01-02"), context, input.SystemPrompt, semanticSearchActive),
	}}
	rows, err := s.database().Query(`SELECT role,content,attachments_json,context_json FROM (SELECT role,content,attachments_json,context_json,created_at FROM chat_messages WHERE conversation_id=? ORDER BY created_at DESC LIMIT 40) ORDER BY created_at`, conversationID)
	if err == nil {
		history := []aiMessage{}
		for rows.Next() {
			var message aiMessage
			var attachmentsJSON, contextJSON string
			if rows.Scan(&message.Role, &message.Content, &attachmentsJSON, &contextJSON) == nil {
				_ = json.Unmarshal([]byte(attachmentsJSON), &message.Attachments)
				var expanded []aiMessage
				if message.Role == "assistant" && json.Unmarshal([]byte(contextJSON), &expanded) == nil && len(expanded) > 0 {
					history = append(history, expanded...)
				} else {
					history = append(history, message)
				}
			}
		}
		rows.Close()
		messages = append(messages, history...)
	}
	messages = append(messages, aiMessage{Role: "user", Content: input.Message, Attachments: input.Attachments})
	userID, _ := randomToken()
	attachmentsJSON, _ := json.Marshal(input.Attachments)
	_, _ = s.database().Exec(`INSERT INTO chat_messages(id,conversation_id,role,content,created_at,attachments_json,sources_json,tools_json,context_json) VALUES(?,?,?,?,?,?,'[]','[]','[]')`, userID, conversationID, "user", input.Message, now, string(attachmentsJSON))
	flusher, ok := beginAIStream(w)
	if !ok {
		return
	}
	writeAIEvent(w, flusher, map[string]any{"type": "start", "conversationId": conversationID, "sources": []chatSource{}})
	var full strings.Builder
	turnContext := []aiMessage{}
	activities := []chatToolActivity{}
	sourceByID := map[string]chatSource{}
	definitions := chatToolDefinitions(semanticSearchActive)
	for round := 0; round < maxChatToolRounds; round++ {
		activeTools := definitions
		if round == maxChatToolRounds-1 {
			activeTools = nil
		}
		roundMessages := fitChatMessages(messages, config.Provider, config.Model)
		roundTextStart := full.Len()
		result, roundErr := streamProviderRound(r.Context(), config, apiKey, roundMessages, activeTools, func(delta string) {
			full.WriteString(delta)
			writeAIEvent(w, flusher, map[string]any{"type": "text-delta", "text": delta})
		})
		if roundErr != nil {
			if full.Len() > roundTextStart {
				turnContext = append(turnContext, aiMessage{
					Role:    "assistant",
					Content: full.String()[roundTextStart:],
				})
			}
			if full.Len() > 0 || len(activities) > 0 {
				s.persistAssistantTurn(conversationID, full.String(), activities, sourceByID, turnContext)
			}
			writeAIEvent(w, flusher, map[string]any{"type": "error", "message": roundErr.Error()})
			return
		}
		messages = append(messages, result.Assistant)
		turnContext = append(turnContext, result.Assistant)
		if len(result.ToolCalls) == 0 {
			break
		}
		for _, call := range result.ToolCalls {
			var inputMap map[string]any
			_ = json.Unmarshal([]byte(call.Arguments), &inputMap)
			writeAIEvent(w, flusher, map[string]any{
				"type":       "tool-call",
				"toolCallId": call.ID,
				"tool":       call.Name,
				"input":      inputMap,
				"summary":    toolCallSummary(call.Name, inputMap),
			})
			execution := s.executeChatTool(input.NotebookID, call, semanticSearchActive)
			execution.Activity.TextOffset = utf8.RuneCountInString(full.String())
			activities = append(activities, execution.Activity)
			for _, source := range execution.Activity.Sources {
				sourceByID[source.ID] = source
			}
			toolMessage := aiMessage{
				Role:       "tool",
				Content:    execution.Output,
				ToolCallID: call.ID,
			}
			messages = append(messages, toolMessage)
			turnContext = append(turnContext, toolMessage)
			event := map[string]any{
				"type":       "tool-result",
				"toolCallId": call.ID,
				"tool":       call.Name,
				"summary":    execution.Activity.Summary,
				"sources":    execution.Activity.Sources,
			}
			if execution.Activity.Error != "" {
				event["message"] = execution.Activity.Error
			}
			writeAIEvent(w, flusher, event)
		}
	}
	if strings.TrimSpace(full.String()) == "" {
		notice := "模型完成了检索，但没有生成回答。请缩小问题范围后重试。"
		full.WriteString(notice)
		last := len(turnContext) - 1
		if last >= 0 && turnContext[last].Role == "assistant" && len(turnContext[last].ToolCalls) == 0 {
			turnContext[last].Content = notice
		} else {
			turnContext = append(turnContext, aiMessage{Role: "assistant", Content: notice})
		}
		writeAIEvent(w, flusher, map[string]any{"type": "text-delta", "text": notice})
	}
	assistantID, _ := randomToken()
	doneAt := time.Now().Format(time.RFC3339Nano)
	sources := make([]chatSource, 0, len(sourceByID))
	for _, source := range sourceByID {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Title < sources[j].Title
	})
	sourcesJSON, _ := json.Marshal(sources)
	toolsJSON, _ := json.Marshal(activities)
	contextJSON, _ := json.Marshal(turnContext)
	_, _ = s.database().Exec(`INSERT INTO chat_messages(id,conversation_id,role,content,created_at,sources_json,tools_json,context_json) VALUES(?,?,?,?,?,?,?,?)`, assistantID, conversationID, "assistant", full.String(), doneAt, string(sourcesJSON), string(toolsJSON), string(contextJSON))
	_, _ = s.database().Exec(`UPDATE chat_conversations SET updated_at=? WHERE id=?`, doneAt, conversationID)
	writeAIEvent(w, flusher, map[string]any{"type": "complete", "text": full.String(), "sources": sources})
}

func (s *server) persistAssistantTurn(conversationID, content string, activities []chatToolActivity, sourceByID map[string]chatSource, turnContext []aiMessage) {
	assistantID, _ := randomToken()
	doneAt := time.Now().Format(time.RFC3339Nano)
	sources := make([]chatSource, 0, len(sourceByID))
	for _, source := range sourceByID {
		sources = append(sources, source)
	}
	sourcesJSON, _ := json.Marshal(sources)
	toolsJSON, _ := json.Marshal(activities)
	contextJSON, _ := json.Marshal(turnContext)
	_, _ = s.database().Exec(`INSERT INTO chat_messages(id,conversation_id,role,content,created_at,sources_json,tools_json,context_json) VALUES(?,?,?,?,?,?,?,?)`, assistantID, conversationID, "assistant", content, doneAt, string(sourcesJSON), string(toolsJSON), string(contextJSON))
	_, _ = s.database().Exec(`UPDATE chat_conversations SET updated_at=? WHERE id=?`, doneAt, conversationID)
}

func (s *server) aiConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	notebookID := r.URL.Query().Get("notebookId")
	rows, err := s.database().Query(`SELECT id,notebook_id,title,created_at,updated_at FROM chat_conversations WHERE (?='' OR notebook_id=?) ORDER BY updated_at DESC LIMIT 50`, notebookID, notebookID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	items := []chatConversation{}
	for rows.Next() {
		var item chatConversation
		if rows.Scan(&item.ID, &item.NotebookID, &item.Title, &item.CreatedAt, &item.UpdatedAt) == nil {
			items = append(items, item)
		}
	}
	jsonOut(w, items)
}

func (s *server) aiConversation(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/conversations/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "conversation id required", 400)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := s.database().Query(`SELECT id,conversation_id,role,content,created_at,attachments_json,sources_json,tools_json FROM chat_messages WHERE conversation_id=? ORDER BY created_at`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		items := []chatMessage{}
		for rows.Next() {
			var item chatMessage
			var attachmentsJSON, sourcesJSON, toolsJSON string
			if rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &item.CreatedAt, &attachmentsJSON, &sourcesJSON, &toolsJSON) == nil {
				_ = json.Unmarshal([]byte(attachmentsJSON), &item.Attachments)
				_ = json.Unmarshal([]byte(sourcesJSON), &item.Sources)
				_ = json.Unmarshal([]byte(toolsJSON), &item.Tools)
				items = append(items, item)
			}
		}
		jsonOut(w, items)
	case http.MethodDelete:
		result, err := s.database().Exec(`DELETE FROM chat_conversations WHERE id=?`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			http.Error(w, "conversation not found", 404)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func streamProvider(r *http.Request, config aiProviderConfig, apiKey string, messages []aiMessage, onDelta func(string)) (string, error) {
	endpoint := ""
	headers := map[string]string{"Content-Type": "application/json"}
	var payload any
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
		endpoint = strings.TrimRight(base, "/") + "/chat/completions"
		headers["Authorization"] = "Bearer " + apiKey
		payload = map[string]any{"model": config.Model, "messages": messages, "stream": true}
	case "anthropic":
		base := config.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		endpoint = strings.TrimRight(base, "/") + "/v1/messages"
		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
		system := ""
		providerMessages := []aiMessage{}
		for _, message := range messages {
			if message.Role == "system" {
				system += message.Content + "\n"
			} else {
				providerMessages = append(providerMessages, message)
			}
		}
		payload = map[string]any{"model": config.Model, "system": system, "messages": providerMessages, "max_tokens": 4096, "stream": true}
	case "google":
		base := config.BaseURL
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
		endpoint = strings.TrimRight(base, "/") + "/models/" + url.PathEscape(config.Model) + ":streamGenerateContent?alt=sse&key=" + url.QueryEscape(apiKey)
		system := ""
		contents := []map[string]any{}
		for _, message := range messages {
			if message.Role == "system" {
				system += message.Content + "\n"
				continue
			}
			role := message.Role
			if role == "assistant" {
				role = "model"
			}
			contents = append(contents, map[string]any{"role": role, "parts": []map[string]string{{"text": message.Content}}})
		}
		payload = map[string]any{"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}}, "contents": contents}
	default:
		return "", errors.New("unsupported AI provider")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return "", fmt.Errorf("%s returned %d: %s", config.Label, response.StatusCode, strings.TrimSpace(string(failure)))
	}
	var full strings.Builder
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		delta := providerDelta(config.Provider, []byte(data))
		if delta != "" {
			full.WriteString(delta)
			onDelta(delta)
		}
	}
	if err = scanner.Err(); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

func providerDelta(provider string, data []byte) string {
	switch provider {
	case "openai", "openrouter":
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		_ = json.Unmarshal(data, &event)
		if len(event.Choices) > 0 {
			return event.Choices[0].Delta.Content
		}
	case "anthropic":
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		_ = json.Unmarshal(data, &event)
		if event.Type == "content_block_delta" {
			return event.Delta.Text
		}
	case "google":
		var event struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		_ = json.Unmarshal(data, &event)
		if len(event.Candidates) > 0 {
			var result strings.Builder
			for _, part := range event.Candidates[0].Content.Parts {
				result.WriteString(part.Text)
			}
			return result.String()
		}
	}
	return ""
}
