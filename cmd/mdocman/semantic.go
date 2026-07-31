package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	semanticTargetChars    = 1000
	semanticMinimumChars   = 200
	semanticAssetTextLimit = 24000
	semanticIndexVersion   = "sentence-chunks-v1"
	semanticCandidateLimit = 24
	semanticMinimumScore   = 0.3
)

var (
	semanticHeadingPattern = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t]*$`)
	semanticBreakPattern   = regexp.MustCompile(`[.!?。！？][)"'”’】）]?[ \t\r\n]+|\n{2,}`)
	semanticAssetPattern   = regexp.MustCompile(`(?:^|[\s("'=])/?uploads/([^\s)"'>]+)`)
	errSemanticNotReady    = errors.New("semantic index is not ready")
)

type semanticIndexStatus struct {
	Enabled    bool   `json:"enabled"`
	Available  bool   `json:"available"`
	Status     string `json:"status"`
	Model      string `json:"model,omitempty"`
	Indexed    int    `json:"indexed"`
	Total      int    `json:"total"`
	Message    string `json:"message,omitempty"`
	LastUpdate string `json:"lastUpdate,omitempty"`
}

type semanticTextChunk struct {
	Heading string
	From    int
	To      int
	Text    string
}

type semanticDocument struct {
	ID         string
	NotebookID string
	Title      string
	Content    string
	Trashed    bool
	Private    bool
}

type semanticSearchHit struct {
	DocumentID string
	Title      string
	Heading    string
	Snippet    string
	UpdatedAt  string
	Score      float64
}

type semanticService struct {
	mu            sync.RWMutex
	embedMu       sync.Mutex
	embedder      semanticEmbedder
	enabled       bool
	running       bool
	rerun         bool
	force         bool
	targetDB      *sql.DB
	targetUploads string
	readyDB       *sql.DB
	status        semanticIndexStatus
}

func newSemanticService() *semanticService {
	return newSemanticServiceWithEmbedder(newPlatformSemanticEmbedder())
}

func newSemanticServiceWithEmbedder(embedder semanticEmbedder) *semanticService {
	available := embedder != nil && embedder.Available()
	status := semanticIndexStatus{
		Available: available,
		Status:    "disabled",
	}
	if available {
		status.Model = embedder.DisplayName()
	} else {
		status.Status = "unavailable"
		status.Message = "当前平台没有可用的本地句向量运行时。"
	}
	return &semanticService{embedder: embedder, status: status}
}

func (service *semanticService) configure(enabled bool) semanticIndexStatus {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.enabled = enabled && service.status.Available
	service.status.Enabled = service.enabled
	if !service.status.Available {
		service.status.Status = "unavailable"
		return service.status
	}
	if !service.enabled {
		service.status.Status = "disabled"
		service.status.Message = ""
		return service.status
	}
	if !service.running && service.status.Status != "ready" {
		service.status.Status = "idle"
		service.status.Message = ""
	}
	return service.status
}

func (service *semanticService) snapshot(db *sql.DB) semanticIndexStatus {
	service.mu.RLock()
	defer service.mu.RUnlock()
	status := service.status
	if status.Status == "ready" && service.readyDB != db {
		status.Status = "idle"
		status.Indexed = 0
		status.Total = 0
	}
	return status
}

func (service *semanticService) readyFor(db *sql.DB) bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.enabled && service.status.Status == "ready" && service.readyDB == db
}

func (service *semanticService) requestRebuild(db *sql.DB, uploadsDir string, force bool) {
	service.mu.Lock()
	if !service.enabled || !service.status.Available || db == nil {
		service.mu.Unlock()
		return
	}
	service.targetDB = db
	service.targetUploads = uploadsDir
	service.force = service.force || force
	service.status.Status = "indexing"
	service.status.Message = ""
	if service.running {
		service.rerun = true
		service.mu.Unlock()
		return
	}
	service.running = true
	service.mu.Unlock()
	go service.rebuildLoop()
}

func (service *semanticService) rebuildLoop() {
	for {
		service.mu.Lock()
		db := service.targetDB
		uploadsDir := service.targetUploads
		force := service.force
		service.force = false
		service.rerun = false
		service.status.Status = "indexing"
		service.status.Indexed = 0
		service.status.Total = 0
		service.mu.Unlock()

		indexed, total, err := service.rebuild(db, uploadsDir, force)

		service.mu.Lock()
		if !service.enabled {
			service.running = false
			service.status.Status = "disabled"
			service.status.Message = ""
			service.mu.Unlock()
			return
		}
		if service.rerun {
			service.mu.Unlock()
			continue
		}
		service.running = false
		service.status.Indexed = indexed
		service.status.Total = total
		service.status.LastUpdate = time.Now().Format(time.RFC3339)
		if err != nil {
			service.status.Status = "failed"
			service.status.Message = err.Error()
		} else {
			service.status.Status = "ready"
			service.status.Message = ""
			service.readyDB = db
		}
		service.mu.Unlock()
		return
	}
}

func (service *semanticService) updateProgress(db *sql.DB, indexed, total int) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.targetDB == db && service.status.Status == "indexing" {
		service.status.Indexed = indexed
		service.status.Total = total
	}
}

func (service *semanticService) stillEnabled() bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.enabled
}

func (service *semanticService) rebuild(db *sql.DB, uploadsDir string, force bool) (int, int, error) {
	rows, err := db.Query(`SELECT id,notebook_id,title,content,trashed,private FROM documents ORDER BY notebook_id,id`)
	if err != nil {
		return 0, 0, err
	}
	documents := []semanticDocument{}
	allIDs := map[string]bool{}
	for rows.Next() {
		var document semanticDocument
		var trashed, private int
		if err = rows.Scan(&document.ID, &document.NotebookID, &document.Title, &document.Content, &trashed, &private); err != nil {
			rows.Close()
			return 0, 0, err
		}
		document.Trashed = trashed != 0
		document.Private = private != 0 || frontmatterPrivate(document.Content)
		allIDs[document.ID] = true
		if !document.Trashed && !document.Private {
			documents = append(documents, document)
		} else if err = removeSemanticDocument(db, document.ID); err != nil {
			rows.Close()
			return 0, 0, err
		}
	}
	if err = rows.Close(); err != nil {
		return 0, 0, err
	}
	if err = removeOrphanSemanticDocuments(db, allIDs); err != nil {
		return 0, 0, err
	}
	if force {
		if _, err = db.Exec(`DELETE FROM semantic_chunks; DELETE FROM semantic_documents`); err != nil {
			return 0, len(documents), err
		}
	}
	indexed := 0
	total := len(documents)
	service.updateProgress(db, indexed, total)
	for _, document := range documents {
		if !service.stillEnabled() {
			return indexed, total, errors.New("semantic indexing was disabled")
		}
		if _, err = service.indexDocument(db, uploadsDir, document, force); err != nil {
			return indexed, total, fmt.Errorf("index %q: %w", document.Title, err)
		}
		indexed++
		service.updateProgress(db, indexed, total)
	}
	return indexed, total, nil
}

func removeOrphanSemanticDocuments(db *sql.DB, allIDs map[string]bool) error {
	rows, err := db.Query(`SELECT document_id FROM semantic_documents`)
	if err != nil {
		return err
	}
	orphaned := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !allIDs[id] {
			orphaned = append(orphaned, id)
		}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, id := range orphaned {
		if err = removeSemanticDocument(db, id); err != nil {
			return err
		}
	}
	return nil
}

func removeSemanticDocument(db *sql.DB, documentID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM semantic_chunks WHERE document_id=?`, documentID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM semantic_documents WHERE document_id=?`, documentID); err != nil {
		return err
	}
	return tx.Commit()
}

func (service *semanticService) indexDocument(db *sql.DB, uploadsDir string, document semanticDocument, force bool) (bool, error) {
	assets := semanticAssetDescriptions(uploadsDir, document.Content)
	hash := semanticDocumentHash(document, assets)
	indexID := semanticIndexVersion + "|" + service.embedder.DisplayName()
	if !force {
		var storedHash, storedIndex string
		err := db.QueryRow(`SELECT content_hash,index_version FROM semantic_documents WHERE document_id=?`, document.ID).Scan(&storedHash, &storedIndex)
		if err == nil && storedHash == hash && storedIndex == indexID {
			return false, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return false, err
		}
	}
	chunks := semanticChunks(document.Content, assets)
	embedded := make([]semanticEmbedding, 0, len(chunks))
	for _, chunk := range chunks {
		service.embedMu.Lock()
		vector, err := service.embedder.Embed(chunk.Text)
		service.embedMu.Unlock()
		if err != nil {
			return false, err
		}
		embedded = append(embedded, vector)
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM semantic_chunks WHERE document_id=?`, document.ID); err != nil {
		return false, err
	}
	for index, chunk := range chunks {
		vector := embedded[index]
		if _, err = tx.Exec(`INSERT INTO semantic_chunks(document_id,notebook_id,heading,pos_from,pos_to,text,content_hash,model_id,language,vector) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			document.ID,
			document.NotebookID,
			chunk.Heading,
			chunk.From,
			chunk.To,
			chunk.Text,
			semanticTextHash(chunk.Text),
			vector.ModelID,
			vector.Language,
			encodeSemanticVector(vector.Values),
		); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(`INSERT INTO semantic_documents(document_id,notebook_id,content_hash,index_version,indexed_at) VALUES(?,?,?,?,?) ON CONFLICT(document_id) DO UPDATE SET notebook_id=excluded.notebook_id,content_hash=excluded.content_hash,index_version=excluded.index_version,indexed_at=excluded.indexed_at`,
		document.ID, document.NotebookID, hash, indexID, time.Now().Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (service *semanticService) search(db *sql.DB, notebookID, query string, limit int) ([]semanticSearchHit, error) {
	if !service.readyFor(db) {
		return nil, errSemanticNotReady
	}
	service.embedMu.Lock()
	queryVector, err := service.embedder.Embed(query)
	service.embedMu.Unlock()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT c.document_id,d.title,d.content,c.heading,c.text,d.updated_at,c.vector
FROM semantic_chunks c JOIN documents d ON d.id=c.document_id
WHERE c.notebook_id=? AND c.model_id=? AND c.language=?
  AND d.notebook_id=? AND d.trashed=0 AND d.private=0`,
		notebookID, queryVector.ModelID, queryVector.Language, notebookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[string]semanticSearchHit{}
	for rows.Next() {
		var documentID, title, content, heading, text, updatedAt string
		var encoded []byte
		if err = rows.Scan(&documentID, &title, &content, &heading, &text, &updatedAt, &encoded); err != nil {
			return nil, err
		}
		if frontmatterPrivate(content) {
			continue
		}
		vector, decodeErr := decodeSemanticVector(encoded)
		if decodeErr != nil || len(vector) != len(queryVector.Values) {
			continue
		}
		score := semanticDot(queryVector.Values, vector)
		if score < semanticMinimumScore {
			continue
		}
		current, exists := best[documentID]
		if !exists || score > current.Score {
			best[documentID] = semanticSearchHit{
				DocumentID: documentID,
				Title:      title,
				Heading:    heading,
				Snippet:    strings.TrimSpace(text),
				UpdatedAt:  updatedAt,
				Score:      score,
			}
		}
	}
	hits := make([]semanticSearchHit, 0, len(best))
	for _, hit := range best {
		hits = append(hits, hit)
	}
	sort.Slice(hits, func(left, right int) bool {
		if hits[left].Score == hits[right].Score {
			return hits[left].Title < hits[right].Title
		}
		return hits[left].Score > hits[right].Score
	})
	candidateLimit := semanticCandidateLimit
	if limit > candidateLimit {
		candidateLimit = limit
	}
	if len(hits) > candidateLimit {
		hits = hits[:candidateLimit]
	}
	return hits, rows.Err()
}

func semanticDocumentHash(document semanticDocument, assets []semanticAssetDescription) string {
	hash := sha256.New()
	hash.Write([]byte(document.Title))
	hash.Write([]byte{0})
	hash.Write([]byte(document.Content))
	for _, asset := range assets {
		hash.Write([]byte{0})
		hash.Write([]byte(asset.Name))
		hash.Write([]byte{0})
		hash.Write([]byte(asset.Text))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func semanticTextHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", hash[:])
}

type semanticAssetDescription struct {
	Name string
	Text string
}

func semanticAssetDescriptions(uploadsDir, content string) []semanticAssetDescription {
	seen := map[string]bool{}
	result := []semanticAssetDescription{}
	remaining := semanticAssetTextLimit
	for _, match := range semanticAssetPattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 2 || remaining <= 0 {
			break
		}
		decoded, err := url.PathUnescape(match[1])
		if err != nil {
			decoded = match[1]
		}
		name := filepath.Base(decoded)
		if name == "" || name == "." || seen[name] {
			continue
		}
		seen[name] = true
		description, err := os.ReadFile(filepath.Join(uploadsDir, name+".reflect.md"))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(stripFrontmatter(string(description)))
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > remaining {
			text = truncateRunesOnly(text, remaining)
		}
		remaining -= utf8.RuneCountInString(text)
		result = append(result, semanticAssetDescription{Name: name, Text: text})
	}
	return result
}

func semanticChunks(content string, assets []semanticAssetDescription) []semanticTextChunk {
	body := stripFrontmatter(content)
	chunks := semanticMarkdownChunks(body, 0)
	offset := len(body) + 1
	for _, asset := range assets {
		assetChunks := semanticSectionChunks(asset.Text, offset, asset.Name)
		assetChunks = mergeSemanticTail(assetChunks, asset.Text, offset)
		chunks = append(chunks, assetChunks...)
		offset += len(asset.Text) + 2
	}
	return chunks
}

func semanticMarkdownChunks(body string, base int) []semanticTextChunk {
	headings := semanticHeadingPattern.FindAllStringSubmatchIndex(body, -1)
	chunks := []semanticTextChunk{}
	if len(headings) == 0 {
		return mergeSemanticTail(semanticSectionChunks(body, base, ""), body, base)
	}
	if headings[0][0] > 0 {
		chunks = append(chunks, semanticSectionChunks(body[:headings[0][0]], base, "")...)
	}
	for index, heading := range headings {
		end := len(body)
		if index+1 < len(headings) {
			end = headings[index+1][0]
		}
		label := strings.TrimSpace(body[heading[2]:heading[3]])
		chunks = append(chunks, semanticSectionChunks(body[heading[0]:end], base+heading[0], label)...)
	}
	return mergeSemanticTail(chunks, body, base)
}

func semanticSectionChunks(text string, base int, heading string) []semanticTextChunk {
	breaks := semanticBreakPattern.FindAllStringIndex(text, -1)
	spans := make([][2]int, 0, len(breaks)+1)
	start := 0
	for _, boundary := range breaks {
		spans = append(spans, [2]int{start, boundary[1]})
		start = boundary[1]
	}
	if start < len(text) {
		spans = append(spans, [2]int{start, len(text)})
	}
	chunks := []semanticTextChunk{}
	chunkStart := -1
	chunkEnd := -1
	flush := func() {
		if chunkStart < 0 {
			return
		}
		value := text[chunkStart:chunkEnd]
		if strings.TrimSpace(value) != "" {
			chunks = append(chunks, semanticTextChunk{
				Heading: heading,
				From:    base + chunkStart,
				To:      base + chunkEnd,
				Text:    value,
			})
		}
		chunkStart = -1
	}
	for _, span := range spans {
		if chunkStart < 0 {
			chunkStart = span[0]
		}
		chunkEnd = span[1]
		if utf8.RuneCountInString(text[chunkStart:chunkEnd]) >= semanticTargetChars {
			flush()
		}
	}
	flush()
	return chunks
}

func mergeSemanticTail(chunks []semanticTextChunk, source string, base int) []semanticTextChunk {
	if len(chunks) < 2 {
		return chunks
	}
	last := chunks[len(chunks)-1]
	previous := chunks[len(chunks)-2]
	if utf8.RuneCountInString(last.Text) >= semanticMinimumChars || last.Heading != previous.Heading {
		return chunks
	}
	from := previous.From - base
	to := last.To - base
	if from < 0 || to > len(source) || from >= to {
		return chunks
	}
	merged := semanticTextChunk{
		Heading: previous.Heading,
		From:    previous.From,
		To:      last.To,
		Text:    source[from:to],
	}
	return append(chunks[:len(chunks)-2], merged)
}

func truncateRunesOnly(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func encodeSemanticVector(values []float32) []byte {
	encoded := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(encoded[index*4:], math.Float32bits(value))
	}
	return encoded
}

func decodeSemanticVector(encoded []byte) ([]float32, error) {
	if len(encoded)%4 != 0 {
		return nil, errors.New("invalid stored sentence vector")
	}
	values := make([]float32, len(encoded)/4)
	for index := range values {
		values[index] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[index*4:]))
	}
	return values, nil
}

func semanticDot(left, right []float32) float64 {
	var result float64
	for index := range left {
		result += float64(left[index]) * float64(right[index])
	}
	return result
}

func (s *server) semanticRuntime() *semanticService {
	s.semanticOnce.Do(func() {
		if s.semantic == nil {
			s.semantic = newSemanticService()
		}
	})
	return s.semantic
}

func (s *server) semanticSettings(w http.ResponseWriter, r *http.Request) {
	runtime := s.semanticRuntime()
	db := s.database()
	switch r.Method {
	case http.MethodGet:
		jsonOut(w, runtime.snapshot(db))
	case http.MethodPut:
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		runtime.configure(input.Enabled)
		if input.Enabled {
			runtime.requestRebuild(db, s.uploadsDir(), false)
		}
		jsonOut(w, runtime.snapshot(db))
	case http.MethodPost:
		status := runtime.configure(true)
		if !status.Available {
			http.Error(w, status.Message, http.StatusNotImplemented)
			return
		}
		runtime.requestRebuild(db, s.uploadsDir(), true)
		jsonOut(w, runtime.snapshot(db))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
