package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxChatToolRounds   = 12
	maxChatSearchHits   = 20
	maxChatReadNotes    = 10
	maxChatNoteChars    = 24000
	maxChatSystemPrompt = 20000
	chatContextCeiling  = 200000
	chatTurnReserve     = 60000
	maxChatAttachments  = 4
	maxChatImageBytes   = 5 * 1024 * 1024
	maxChatImageTotal   = 12 * 1024 * 1024
)

type chatAttachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	DataURL   string `json:"dataUrl"`
}

type aiToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type aiToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatSource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type chatToolActivity struct {
	ToolCallID string         `json:"toolCallId"`
	Tool       string         `json:"tool"`
	Summary    string         `json:"summary"`
	Status     string         `json:"status"`
	TextOffset int            `json:"textOffset,omitempty"`
	Sources    []chatSource   `json:"sources,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Error      string         `json:"error,omitempty"`
}

func chatToolDefinitions(semanticSearchEnabled bool) []aiToolDefinition {
	searchDescription := "Search the current notebook with lexical full-text search over note titles and bodies. Returns matching note IDs, exact titles, and short snippets. Queries are plain language; private and trashed notes are excluded."
	queryDescription := "Words or names likely present in the user's notes"
	if semanticSearchEnabled {
		searchDescription = "Search the current notebook by both keywords and meaning using local hybrid retrieval. Returns matching note IDs, exact titles, headings, and focused snippets. Rewording the same concept should return the same notes; private and trashed notes are excluded."
		queryDescription = "The topic, question, phrase, or concept to find in the user's notes"
	}
	return []aiToolDefinition{
		{
			Name:        "search_notes",
			Description: searchDescription,
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{"type": "string", "description": queryDescription},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxChatSearchHits, "description": "Number of matches, default 8"},
			}, []string{"query"}),
		},
		{
			Name:        "list_recent_notes",
			Description: "List recently edited non-daily notes in the current notebook, newest first. Omit tag unless the user named a tag. Private and trashed notes are excluded.",
			Parameters: objectSchema(map[string]any{
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxChatSearchHits, "description": "Number of notes, default 10"},
				"tag":   map[string]any{"type": []string{"string", "null"}, "description": "Optional tag without #"},
			}, nil),
		},
		{
			Name:        "list_daily_notes",
			Description: "List daily notes in an inclusive ISO date range. Use for questions about yesterday, last week, or another period. At most 31 written days are returned; private notes are excluded.",
			Parameters: objectSchema(map[string]any{
				"start": map[string]any{"type": "string", "description": "First date, YYYY-MM-DD"},
				"end":   map[string]any{"type": "string", "description": "Last date, YYYY-MM-DD"},
			}, []string{"start", "end"}),
		},
		{
			Name:        "read_notes",
			Description: "Read the full Markdown bodies of up to 10 notes from the current notebook by document ID. Pass all required IDs in one call. Private notes cannot be read.",
			Parameters: objectSchema(map[string]any{
				"documentIds": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"minItems":    1,
					"maxItems":    maxChatReadNotes,
					"description": "Document IDs copied from search or listing results",
				},
			}, []string{"documentIds"}),
		},
		{
			Name:        "read_assets",
			Description: "Read stored AI descriptions and text transcriptions for image or PDF attachments referenced as /uploads/... in notes. Returns text, not file bytes. Private or unreferenced assets cannot be read.",
			Parameters: objectSchema(map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"minItems":    1,
					"maxItems":    maxChatReadNotes,
					"description": "Attachment paths copied exactly from note Markdown",
				},
			}, []string{"paths"}),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

type chatGraphContext struct {
	Name          string
	NoteCount     int
	DailyCount    int
	EarliestDaily string
	LatestDaily   string
	Tags          []string
}

func (s *server) loadChatGraphContext(notebookID string) (chatGraphContext, error) {
	var context chatGraphContext
	if err := s.database().QueryRow(`SELECT title FROM notebooks WHERE id=?`, notebookID).Scan(&context.Name); err != nil {
		return context, err
	}
	rows, err := s.database().Query(`SELECT id,content FROM documents WHERE notebook_id=? AND trashed=0 AND private=0`, notebookID)
	if err != nil {
		return context, err
	}
	defer rows.Close()
	tagCounts := map[string]int{}
	for rows.Next() {
		var id, content string
		if err = rows.Scan(&id, &content); err != nil {
			return context, err
		}
		if frontmatterPrivate(content) {
			continue
		}
		context.NoteCount++
		if strings.HasPrefix(id, "daily-") {
			date := strings.TrimPrefix(id, "daily-")
			context.DailyCount++
			if context.EarliestDaily == "" || date < context.EarliestDaily {
				context.EarliestDaily = date
			}
			if date > context.LatestDaily {
				context.LatestDaily = date
			}
		}
		for _, tag := range markdownTags(content) {
			tagCounts[tag]++
		}
	}
	if err = rows.Err(); err != nil {
		return context, err
	}
	type tagCount struct {
		tag   string
		count int
	}
	tags := make([]tagCount, 0, len(tagCounts))
	for tag, count := range tagCounts {
		tags = append(tags, tagCount{tag: tag, count: count})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].count == tags[j].count {
			return tags[i].tag < tags[j].tag
		}
		return tags[i].count > tags[j].count
	})
	if len(tags) > 40 {
		tags = tags[:40]
	}
	for _, item := range tags {
		context.Tags = append(context.Tags, fmt.Sprintf("#%s (%d)", item.tag, item.count))
	}
	return context, nil
}

func toolChatSystemPrompt(today string, context chatGraphContext, custom string, semanticSearchEnabled bool) string {
	lines := []string{
		"You are Reflect's assistant, embedded in the user's personal note graph.",
		"Today's date is " + today + ". Daily notes have document IDs named daily/YYYY-MM-DD in the conceptual graph and daily-YYYY-MM-DD in tool results.",
		"",
		"Graph overview (private notes are excluded from every figure):",
		fmt.Sprintf("- Graph: %q — %d notes and %d daily notes.", context.Name, context.NoteCount, context.DailyCount),
	}
	if context.EarliestDaily != "" && context.LatestDaily != "" {
		lines = append(lines, "- Daily notes span "+context.EarliestDaily+" to "+context.LatestDaily+".")
	}
	if len(context.Tags) == 0 {
		lines = append(lines, "- No tags are in use. Never invent a tag filter.")
	} else {
		lines = append(lines, "- Most-used tags: "+strings.Join(context.Tags, ", ")+".")
	}
	searchGuidance := "- search_notes uses lexical full-text search over titles and note bodies. Choose the user's likely wording, names, and keywords; if the first search is too narrow, try one broader lexical query."
	if semanticSearchEnabled {
		searchGuidance = "- search_notes matches on both keywords and meaning using an on-device index, so it can find relevant notes even when the wording differs. Rewording or reordering the same concept should return the same notes."
	}
	lines = append(lines,
		"",
		"Grounding rules:",
		"- When the user's notes could answer the question, use the tools before answering. search_notes finds notes by topic or keyword; list_daily_notes handles date ranges; list_recent_notes handles recently edited notes. Use read_notes for full content.",
		searchGuidance,
		"- Search can match attachment descriptions folded into note text. A Markdown link such as ![scan](/uploads/scan.pdf) does not describe the file; call read_assets when the attachment itself matters.",
		"- Gather efficiently. Increase a search limit or batch document IDs into one read_notes call instead of repeating narrow calls.",
		"- Ground answers only in tool results. If the notes do not cover the question, say so plainly instead of guessing.",
		"- Cite every note you draw on with a wiki link using its exact returned title, for example [[Project Atlas]]. Never invent a note title.",
		"- Private notes are excluded from search and cannot be read. Never speculate about private content.",
		"",
		"Style: answer in concise Markdown. Prefer short paragraphs and lists over headings.",
	)
	custom = strings.TrimSpace(custom)
	custom, _ = truncateRunes(custom, maxChatSystemPrompt)
	if custom != "" {
		lines = append(lines,
			"",
			"User-configured system prompt (follow it unless it conflicts with the grounding and privacy rules above):",
			custom,
		)
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]), true
}

func validateChatAttachments(input []chatAttachment) ([]chatAttachment, error) {
	if len(input) > maxChatAttachments {
		return nil, fmt.Errorf("at most %d images can be attached", maxChatAttachments)
	}
	allowed := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/webp": true,
		"image/gif":  true,
	}
	result := make([]chatAttachment, 0, len(input))
	total := 0
	for _, attachment := range input {
		attachment.Name = strings.TrimSpace(attachment.Name)
		attachment.MediaType = strings.ToLower(strings.TrimSpace(attachment.MediaType))
		if !allowed[attachment.MediaType] {
			return nil, fmt.Errorf("unsupported image type %q", attachment.MediaType)
		}
		prefix := "data:" + attachment.MediaType + ";base64,"
		if !strings.HasPrefix(attachment.DataURL, prefix) {
			return nil, errors.New("image data URL does not match its media type")
		}
		encoded := strings.TrimPrefix(attachment.DataURL, prefix)
		decodedSize := base64.StdEncoding.DecodedLen(len(encoded))
		if decodedSize > maxChatImageBytes {
			return nil, errors.New("an attached image is too large")
		}
		if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
			return nil, errors.New("an attached image is not valid base64")
		}
		total += decodedSize
		if total > maxChatImageTotal {
			return nil, errors.New("attached images are too large in total")
		}
		if attachment.ID == "" {
			attachment.ID, _ = randomToken()
		}
		if attachment.Name == "" {
			attachment.Name = "image"
		}
		result = append(result, attachment)
	}
	return result, nil
}

func chatModelContextTokens(provider, model string) int {
	switch provider {
	case "openai":
		if model == "gpt-5.4-mini" || model == "gpt-5.4-nano" {
			return 400000
		}
		if strings.HasPrefix(model, "gpt-5.") {
			return 1000000
		}
	case "anthropic":
		if model == "claude-haiku-4-5" {
			return 200000
		}
		if strings.HasPrefix(model, "claude-") {
			return 1000000
		}
	case "google":
		if strings.HasPrefix(model, "gemini-") {
			return 1000000
		}
	case "openrouter":
		if model == "openai/gpt-5.2" {
			return 400000
		}
	}
	return 128000
}

func aiMessageChars(message aiMessage) int {
	total := len(message.Content) + len(message.Role) + 16
	total += len(message.Attachments) * 6400
	for _, call := range message.ToolCalls {
		total += len(call.ID) + len(call.Name) + len(call.Arguments) + 32
	}
	return total
}

func aiMessagesChars(messages []aiMessage) int {
	total := 0
	for _, message := range messages {
		total += aiMessageChars(message)
	}
	return total
}

func fitChatMessages(messages []aiMessage, provider, model string) []aiMessage {
	if len(messages) < 2 {
		return messages
	}
	window := chatModelContextTokens(provider, model)
	if window > chatContextCeiling {
		window = chatContextCeiling
	}
	historyBudget := int(float64((window-chatTurnReserve)*4-len(messages[0].Content)) * 0.8)
	if historyBudget < 1 || aiMessagesChars(messages[1:]) <= historyBudget {
		return messages
	}
	segments := [][]aiMessage{}
	for _, message := range messages[1:] {
		if message.Role == "user" || len(segments) == 0 {
			segments = append(segments, []aiMessage{message})
		} else {
			segments[len(segments)-1] = append(segments[len(segments)-1], message)
		}
	}
	for index := range segments {
		if index >= len(segments)-2 {
			continue
		}
		for messageIndex := range segments[index] {
			if segments[index][messageIndex].Role == "tool" {
				segments[index][messageIndex].Content = "[Old tool result elided to fit the context window.]"
			}
		}
	}
	kept := [][]aiMessage{}
	used := 0
	for index := len(segments) - 1; index >= 0; index-- {
		cost := aiMessagesChars(segments[index])
		if len(kept) > 0 && used+cost > historyBudget {
			break
		}
		kept = append([][]aiMessage{segments[index]}, kept...)
		used += cost
	}
	result := []aiMessage{messages[0]}
	for _, segment := range kept {
		result = append(result, segment...)
	}
	return result
}

type chatToolExecution struct {
	Output   string
	Activity chatToolActivity
}

func (s *server) executeChatTool(notebookID string, call aiToolCall, semanticSearchEnabled bool) chatToolExecution {
	input := map[string]any{}
	if err := json.Unmarshal([]byte(call.Arguments), &input); err != nil {
		return failedToolExecution(call, input, "The tool arguments were not valid JSON.")
	}
	var output any
	var sources []chatSource
	var summary string
	var err error
	switch call.Name {
	case "search_notes":
		output, sources, summary, err = s.chatSearchNotes(notebookID, input, semanticSearchEnabled)
	case "list_recent_notes":
		output, sources, summary, err = s.chatListRecentNotes(notebookID, input)
	case "list_daily_notes":
		output, sources, summary, err = s.chatListDailyNotes(notebookID, input)
	case "read_notes":
		output, sources, summary, err = s.chatReadNotes(notebookID, input)
	case "read_assets":
		output, sources, summary, err = s.chatReadAssets(notebookID, input)
	default:
		err = errors.New("This tool is not available.")
	}
	if err != nil {
		return failedToolExecution(call, input, err.Error())
	}
	encoded, marshalErr := json.Marshal(output)
	if marshalErr != nil {
		return failedToolExecution(call, input, marshalErr.Error())
	}
	return chatToolExecution{
		Output: string(encoded),
		Activity: chatToolActivity{
			ToolCallID: call.ID,
			Tool:       call.Name,
			Summary:    summary,
			Status:     "complete",
			Sources:    sources,
			Input:      input,
		},
	}
}

func failedToolExecution(call aiToolCall, input map[string]any, message string) chatToolExecution {
	encoded, _ := json.Marshal(map[string]any{"ok": false, "error": message})
	return chatToolExecution{
		Output: string(encoded),
		Activity: chatToolActivity{
			ToolCallID: call.ID,
			Tool:       call.Name,
			Summary:    toolCallSummary(call.Name, input) + " — " + message,
			Status:     "error",
			Input:      input,
			Error:      message,
		},
	}
}

func toolCallSummary(name string, input map[string]any) string {
	switch name {
	case "search_notes":
		return fmt.Sprintf("搜索“%s”", stringValue(input, "query"))
	case "list_recent_notes":
		if tag := stringValue(input, "tag"); tag != "" {
			return "列出 #" + tag + " 最近笔记"
		}
		return "列出最近笔记"
	case "list_daily_notes":
		return fmt.Sprintf("列出每日笔记 %s – %s", stringValue(input, "start"), stringValue(input, "end"))
	case "read_notes":
		return "读取笔记"
	case "read_assets":
		return "读取附件描述"
	default:
		return "调用 " + name
	}
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func intValue(input map[string]any, key string, fallback, maximum int) int {
	value := fallback
	switch raw := input[key].(type) {
	case float64:
		value = int(raw)
	case json.Number:
		if parsed, err := strconv.Atoi(raw.String()); err == nil {
			value = parsed
		}
	}
	if value < 1 {
		return 1
	}
	if value > maximum {
		return maximum
	}
	return value
}

func stringSliceValue(input map[string]any, key string, maximum int) []string {
	raw, ok := input[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
		if len(result) == maximum {
			break
		}
	}
	return result
}

func notePreview(content string, limit int) string {
	body := strings.TrimSpace(stripFrontmatter(content))
	if truncated, changed := truncateRunes(body, limit); changed {
		return truncated + "…"
	}
	return body
}

type chatRetrievalHit struct {
	DocumentID string
	Title      string
	Snippet    string
	Heading    string
	UpdatedAt  string
}

func (s *server) lexicalChatHits(notebookID, query string, limit int) ([]chatRetrievalHit, error) {
	expression := ftsExpression(query)
	rows, err := s.database().Query(`SELECT d.id,d.title,d.content,d.updated_at FROM documents_fts JOIN documents d ON d.id=documents_fts.document_id WHERE documents_fts MATCH ? AND d.notebook_id=? AND d.trashed=0 AND d.private=0 ORDER BY bm25(documents_fts),d.updated_at DESC LIMIT ?`, expression, notebookID, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []chatRetrievalHit{}
	for rows.Next() && len(hits) < limit {
		var id, title, content, updatedAt string
		if err = rows.Scan(&id, &title, &content, &updatedAt); err != nil {
			return nil, err
		}
		if frontmatterPrivate(content) {
			continue
		}
		hits = append(hits, chatRetrievalHit{
			DocumentID: id,
			Title:      title,
			Snippet:    notePreview(content, 700),
			UpdatedAt:  updatedAt,
		})
	}
	return hits, rows.Err()
}

func fuseChatRetrieval(lists [][]chatRetrievalHit, limit int) []chatRetrievalHit {
	const damping = 60.0
	type fusedHit struct {
		hit   chatRetrievalHit
		score float64
	}
	fused := map[string]fusedHit{}
	for _, list := range lists {
		for index, hit := range list {
			entry, exists := fused[hit.DocumentID]
			entry.score += 1 / (damping + float64(index) + 1)
			if !exists || (entry.hit.Heading == "" && hit.Heading != "") {
				entry.hit = hit
			}
			fused[hit.DocumentID] = entry
		}
	}
	items := make([]fusedHit, 0, len(fused))
	for _, item := range fused {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].score == items[right].score {
			return items[left].hit.Title < items[right].hit.Title
		}
		return items[left].score > items[right].score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]chatRetrievalHit, 0, len(items))
	for _, item := range items {
		result = append(result, item.hit)
	}
	return result
}

func (s *server) chatSearchNotes(notebookID string, input map[string]any, semanticSearchEnabled bool) (any, []chatSource, string, error) {
	query := stringValue(input, "query")
	if query == "" {
		return nil, nil, "", errors.New("A non-empty search query is required.")
	}
	limit := intValue(input, "limit", 8, maxChatSearchHits)
	lexical, err := s.lexicalChatHits(notebookID, query, limit)
	if err != nil {
		return nil, nil, "", err
	}
	mode := "lexical"
	ranked := lexical
	if semanticSearchEnabled {
		semantic, semanticErr := s.semanticRuntime().search(s.database(), notebookID, query, limit)
		if semanticErr == nil {
			mode = "hybrid"
			semanticHits := make([]chatRetrievalHit, 0, len(semantic))
			for _, hit := range semantic {
				snippet, _ := truncateRunes(hit.Snippet, 700)
				semanticHits = append(semanticHits, chatRetrievalHit{
					DocumentID: hit.DocumentID,
					Title:      hit.Title,
					Snippet:    snippet,
					Heading:    hit.Heading,
					UpdatedAt:  hit.UpdatedAt,
				})
			}
			ranked = fuseChatRetrieval([][]chatRetrievalHit{lexical, semanticHits}, limit)
		}
	}
	hits := []map[string]any{}
	sources := []chatSource{}
	for _, hit := range ranked {
		item := map[string]any{
			"documentId": hit.DocumentID,
			"title":      hit.Title,
			"snippet":    hit.Snippet,
			"updatedAt":  hit.UpdatedAt,
		}
		if hit.Heading != "" {
			item["heading"] = hit.Heading
		}
		hits = append(hits, item)
		sources = append(sources, chatSource{ID: hit.DocumentID, Title: hit.Title})
	}
	summary := fmt.Sprintf("搜索“%s”（%d 篇）", query, len(hits))
	return map[string]any{"hits": hits, "mode": mode}, sources, summary, nil
}

func (s *server) chatListRecentNotes(notebookID string, input map[string]any) (any, []chatSource, string, error) {
	limit := intValue(input, "limit", 10, maxChatSearchHits)
	tag := strings.TrimPrefix(stringValue(input, "tag"), "#")
	if tag != "" && !validTag(tag) {
		return nil, nil, "", errors.New("Not a tag; omit tag to list all recent notes.")
	}
	rows, err := s.database().Query(`SELECT id,title,content,updated_at FROM documents WHERE notebook_id=? AND trashed=0 AND private=0 AND id NOT LIKE 'daily-%' ORDER BY updated_at DESC`, notebookID)
	if err != nil {
		return nil, nil, "", err
	}
	defer rows.Close()
	notes := []map[string]any{}
	sources := []chatSource{}
	for rows.Next() && len(notes) < limit {
		var id, title, content, updatedAt string
		if err = rows.Scan(&id, &title, &content, &updatedAt); err != nil {
			return nil, nil, "", err
		}
		if frontmatterPrivate(content) || (tag != "" && !containsTag(content, tag)) {
			continue
		}
		notes = append(notes, map[string]any{
			"documentId": id,
			"title":      title,
			"snippet":    notePreview(content, 500),
			"updatedAt":  updatedAt,
		})
		sources = append(sources, chatSource{ID: id, Title: title})
	}
	summary := fmt.Sprintf("列出最近笔记（%d 篇）", len(notes))
	if tag != "" {
		summary = fmt.Sprintf("列出 #%s 最近笔记（%d 篇）", tag, len(notes))
	}
	return map[string]any{"notes": notes}, sources, summary, rows.Err()
}

func (s *server) chatListDailyNotes(notebookID string, input map[string]any) (any, []chatSource, string, error) {
	start := stringValue(input, "start")
	end := stringValue(input, "end")
	startDate, startErr := time.Parse("2006-01-02", start)
	endDate, endErr := time.Parse("2006-01-02", end)
	if startErr != nil || endErr != nil || endDate.Before(startDate) {
		return nil, nil, "", errors.New("start and end must be a valid ascending YYYY-MM-DD range.")
	}
	rows, err := s.database().Query(`SELECT id,title,content,updated_at FROM documents WHERE notebook_id=? AND trashed=0 AND private=0 AND id LIKE 'daily-%' AND substr(id,7) BETWEEN ? AND ? ORDER BY id DESC`, notebookID, start, end)
	if err != nil {
		return nil, nil, "", err
	}
	defer rows.Close()
	days := []map[string]any{}
	sources := []chatSource{}
	truncated := false
	for rows.Next() {
		var id, title, content, updatedAt string
		if err = rows.Scan(&id, &title, &content, &updatedAt); err != nil {
			return nil, nil, "", err
		}
		if frontmatterPrivate(content) {
			continue
		}
		if len(days) == 31 {
			truncated = true
			break
		}
		days = append(days, map[string]any{
			"documentId": id,
			"date":       strings.TrimPrefix(id, "daily-"),
			"title":      title,
			"snippet":    notePreview(content, 500),
			"updatedAt":  updatedAt,
		})
		sources = append(sources, chatSource{ID: id, Title: title})
	}
	summary := fmt.Sprintf("列出每日笔记 %s – %s（%d 天）", start, end, len(days))
	return map[string]any{"days": days, "truncated": truncated}, sources, summary, rows.Err()
}

func (s *server) chatReadNotes(notebookID string, input map[string]any) (any, []chatSource, string, error) {
	ids := stringSliceValue(input, "documentIds", maxChatReadNotes)
	if len(ids) == 0 {
		return nil, nil, "", errors.New("At least one document ID is required.")
	}
	notes := []map[string]any{}
	sources := []chatSource{}
	for _, id := range ids {
		var storedNotebook string
		err := s.database().QueryRow(`SELECT notebook_id FROM documents WHERE id=?`, id).Scan(&storedNotebook)
		if err == sql.ErrNoRows || (err == nil && storedNotebook != notebookID) {
			notes = append(notes, map[string]any{"ok": false, "documentId": id, "error": "No note exists at this ID in the current notebook."})
			continue
		}
		if err != nil {
			return nil, nil, "", err
		}
		doc, err := s.cloudSafeDocument(id)
		if err != nil {
			notes = append(notes, map[string]any{"ok": false, "documentId": id, "error": "This note is private and cannot be read by AI."})
			continue
		}
		body, truncated := truncateRunes(stripFrontmatter(doc.Content), maxChatNoteChars)
		notes = append(notes, map[string]any{
			"ok":         true,
			"documentId": id,
			"title":      doc.Title,
			"content":    body,
			"truncated":  truncated,
		})
		sources = append(sources, chatSource{ID: id, Title: doc.Title})
	}
	summary := fmt.Sprintf("读取笔记（%d 篇）", len(notes))
	return map[string]any{"notes": notes}, sources, summary, nil
}

func (s *server) chatReadAssets(notebookID string, input map[string]any) (any, []chatSource, string, error) {
	paths := stringSliceValue(input, "paths", maxChatReadNotes)
	if len(paths) == 0 {
		return nil, nil, "", errors.New("At least one attachment path is required.")
	}
	assets := []map[string]any{}
	for _, rawPath := range paths {
		name := filepath.Base(strings.TrimSpace(rawPath))
		if name == "." || name == "" || (!strings.Contains(rawPath, "/uploads/") && !strings.HasPrefix(rawPath, "uploads/")) {
			assets = append(assets, map[string]any{"ok": false, "path": rawPath, "error": "Not an /uploads/... attachment path."})
			continue
		}
		var publicRefs int
		like := "%/uploads/" + name + "%"
		if err := s.database().QueryRow(`SELECT COUNT(*) FROM documents WHERE notebook_id=? AND trashed=0 AND private=0 AND content LIKE ?`, notebookID, like).Scan(&publicRefs); err != nil {
			return nil, nil, "", err
		}
		verdict, err := s.classifyAsset(name)
		if err != nil || verdict != "send" || publicRefs == 0 {
			assets = append(assets, map[string]any{"ok": false, "path": rawPath, "error": "This asset cannot be read by AI."})
			continue
		}
		description, err := os.ReadFile(filepath.Join(s.uploadsDir(), name+".reflect.md"))
		if err != nil {
			assets = append(assets, map[string]any{"ok": false, "path": rawPath, "error": "No description exists for this asset yet."})
			continue
		}
		body, truncated := truncateRunes(strings.TrimSpace(stripFrontmatter(string(description))), 8000)
		assets = append(assets, map[string]any{"ok": true, "path": "/uploads/" + name, "description": body, "truncated": truncated})
	}
	return map[string]any{"assets": assets}, nil, fmt.Sprintf("读取附件描述（%d 个）", len(assets)), nil
}

var markdownTagPattern = regexp.MustCompile(`(?m)(?:^|[\s(])#([\p{L}\p{N}_/-]+)`)
var validTagPattern = regexp.MustCompile(`^[\p{L}\p{N}_/-]+$`)

func markdownTags(content string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, match := range markdownTagPattern.FindAllStringSubmatch(stripFrontmatter(content), -1) {
		if len(match) < 2 {
			continue
		}
		tag := strings.ToLower(match[1])
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	return result
}

func validTag(tag string) bool {
	return validTagPattern.MatchString(tag)
}

func containsTag(content, tag string) bool {
	tag = strings.ToLower(tag)
	for _, candidate := range markdownTags(content) {
		if candidate == tag {
			return true
		}
	}
	return false
}
