package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type audioMemoRecord struct {
	ID                   string `json:"id"`
	NotebookID           string `json:"notebookId"`
	RecordedDate         string `json:"recordedDate"`
	FileName             string `json:"fileName"`
	MimeType             string `json:"mimeType"`
	Status               string `json:"status"`
	Error                string `json:"error,omitempty"`
	TranscriptDocumentID string `json:"transcriptDocumentId,omitempty"`
	CreatedAt            string `json:"createdAt"`
}

func audioExtension(mimeType string) string {
	switch strings.Split(mimeType, ";")[0] {
	case "audio/webm":
		return "webm"
	case "audio/ogg":
		return "ogg"
	case "audio/wav", "audio/x-wav":
		return "wav"
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4":
		return "m4a"
	default:
		return "webm"
	}
}

func (s *server) listAudioMemos(notebookID string) ([]audioMemoRecord, error) {
	rows, err := s.database().Query(`SELECT id,notebook_id,recorded_date,file_name,mime_type,status,error,transcript_document_id,created_at FROM audio_memos WHERE (?='' OR notebook_id=?) ORDER BY created_at DESC`, notebookID, notebookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []audioMemoRecord{}
	for rows.Next() {
		var item audioMemoRecord
		if err = rows.Scan(&item.ID, &item.NotebookID, &item.RecordedDate, &item.FileName, &item.MimeType, &item.Status, &item.Error, &item.TranscriptDocumentID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *server) ensureDailyForAudio(tx *sql.Tx, notebookID, date, now string) (string, string, bool, error) {
	id := "daily-" + date
	var content string
	var private int
	err := tx.QueryRow(`SELECT content,private FROM documents WHERE id=? AND notebook_id=? AND trashed=0`, id, notebookID).Scan(&content, &private)
	if err == sql.ErrNoRows {
		var folderID string
		err = tx.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,position LIMIT 1`, notebookID, notebookID+"-daily").Scan(&folderID)
		if err != nil {
			return "", "", false, err
		}
		content = "# " + date + "\n"
		_, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,0,0,0,'[]',0)`, id, notebookID, folderID, date, content, 999999, now, now)
	}
	return id, content, private != 0 || frontmatterPrivate(content), err
}

func appendAudioBacklink(content, id, label, fileName string) string {
	entry := fmt.Sprintf("- [[%s|%s]] · [录音](/audio/%s)", id, label, fileName)
	if strings.Contains(content, "[["+id+"|") {
		return content
	}
	if !strings.Contains(content, "## [[Audio memos]]") {
		return strings.TrimRight(content, "\n") + "\n\n## [[Audio memos]]\n\n" + entry + "\n"
	}
	return strings.TrimRight(content, "\n") + "\n" + entry + "\n"
}

func (s *server) audioMemos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.listAudioMemos(r.URL.Query().Get("notebookId"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonOut(w, items)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		file, header, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "audio file required", 400)
			return
		}
		defer file.Close()
		notebookID := r.FormValue("notebookId")
		if notebookID == "" {
			http.Error(w, "notebook required", 400)
			return
		}
		mimeType := header.Header.Get("Content-Type")
		if !strings.HasPrefix(mimeType, "audio/") {
			http.Error(w, "unsupported audio type", 400)
			return
		}
		id, err := randomToken()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		nowTime := time.Now()
		date := nowTime.Format("2006-01-02")
		fileName := fmt.Sprintf("audio-memo-%s-%s.%s", date, nowTime.Format("150405.000"), audioExtension(mimeType))
		if err = os.MkdirAll(s.audioMemosDir(), 0700); err == nil {
			var output *os.File
			output, err = os.Create(filepath.Join(s.audioMemosDir(), fileName))
			if err == nil {
				_, err = io.Copy(output, file)
				_ = output.Close()
			}
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		now := nowTime.Format(time.RFC3339Nano)
		tx, err := s.database().Begin()
		if err == nil {
			_, err = tx.Exec(`INSERT INTO audio_memos(id,notebook_id,recorded_date,file_name,mime_type,status,error,transcript_document_id,created_at) VALUES(?,?,?,?,?,'pending','','',?)`, id, notebookID, date, fileName, mimeType, now)
		}
		var dailyID, dailyContent string
		if err == nil {
			dailyID, dailyContent, _, err = s.ensureDailyForAudio(tx, notebookID, date, now)
		}
		if err == nil {
			label := "Audio memo " + nowTime.Format("15:04")
			_, err = tx.Exec(`UPDATE documents SET content=?,updated_at=?,revision=revision+1 WHERE id=?`, appendAudioBacklink(dailyContent, id, label, fileName), now, dailyID)
		}
		if err == nil {
			err = tx.Commit()
		} else if tx != nil {
			_ = tx.Rollback()
		}
		if err != nil {
			_ = os.Remove(filepath.Join(s.audioMemosDir(), fileName))
			http.Error(w, err.Error(), 500)
			return
		}
		item := audioMemoRecord{ID: id, NotebookID: notebookID, RecordedDate: date, FileName: fileName, MimeType: mimeType, Status: "pending", CreatedAt: now}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(item)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) transcriptionProvider(id string) (aiProviderConfig, string, error) {
	if id != "" {
		config, key, err := s.configuredProvider(id)
		if err == nil && config.Provider != "openai" && config.Provider != "google" {
			return config, "", fmt.Errorf("this provider cannot transcribe audio")
		}
		return config, key, err
	}
	var selected string
	err := s.database().QueryRow(`SELECT id FROM ai_providers WHERE provider IN ('openai','google') ORDER BY is_default DESC,created_at LIMIT 1`).Scan(&selected)
	if err != nil {
		return aiProviderConfig{}, "", err
	}
	return s.configuredProvider(selected)
}

func transcribeOpenAI(config aiProviderConfig, apiKey, fileName string, audio []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", fileName)
	_, _ = part.Write(audio)
	_ = writer.WriteField("model", "gpt-4o-mini-transcribe")
	_ = writer.Close()
	base := config.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/audio/transcriptions", &body)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("OpenAI transcription failed (%d): %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(responseBody, &result) != nil {
		return "", fmt.Errorf("unrecognized OpenAI transcription response")
	}
	return strings.TrimSpace(result.Text), nil
}

func transcribeGoogle(config aiProviderConfig, apiKey, mimeType string, audio []byte) (string, error) {
	base := config.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	model := config.Model
	if !strings.HasPrefix(model, "gemini-") {
		model = "gemini-2.5-flash"
	}
	endpoint := strings.TrimRight(base, "/") + "/models/" + url.PathEscape(model) + ":generateContent?key=" + url.QueryEscape(apiKey)
	payload, _ := json.Marshal(map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]string{"text": "Transcribe this recording verbatim. Return only the transcript."}, map[string]any{"inline_data": map[string]string{"mime_type": strings.Split(mimeType, ";")[0], "data": base64.StdEncoding.EncodeToString(audio)}}}}}})
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Google transcription failed (%d): %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
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
	if json.Unmarshal(responseBody, &result) != nil || len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("unrecognized Google transcription response")
	}
	return strings.TrimSpace(result.Candidates[0].Content.Parts[0].Text), nil
}

func (s *server) audioMemo(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/audio-memos/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "transcribe" || r.Method != http.MethodPost {
		http.Error(w, "audio memo action not found", 404)
		return
	}
	id := parts[0]
	var providerInput struct {
		ProviderID string `json:"providerId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&providerInput)
	var memo audioMemoRecord
	err := s.database().QueryRow(`SELECT id,notebook_id,recorded_date,file_name,mime_type,status,error,transcript_document_id,created_at FROM audio_memos WHERE id=?`, id).Scan(&memo.ID, &memo.NotebookID, &memo.RecordedDate, &memo.FileName, &memo.MimeType, &memo.Status, &memo.Error, &memo.TranscriptDocumentID, &memo.CreatedAt)
	if err != nil {
		http.Error(w, "audio memo not found", 404)
		return
	}
	daily, err := s.documentByID("daily-" + memo.RecordedDate)
	if err == nil && (daily.Private || frontmatterPrivate(daily.Content)) {
		_, _ = s.database().Exec(`UPDATE audio_memos SET status='blocked',error='The daily note is private; cloud transcription is disabled.' WHERE id=?`, id)
		http.Error(w, "the daily note is private; cloud transcription is disabled", http.StatusForbidden)
		return
	}
	config, apiKey, err := s.transcriptionProvider(providerInput.ProviderID)
	if err != nil {
		http.Error(w, "add an OpenAI or Google provider before transcribing", 400)
		return
	}
	audio, err := os.ReadFile(filepath.Join(s.audioMemosDir(), memo.FileName))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_, _ = s.database().Exec(`UPDATE audio_memos SET status='transcribing',error='' WHERE id=?`, id)
	var transcript string
	if config.Provider == "openai" {
		transcript, err = transcribeOpenAI(config, apiKey, memo.FileName, audio)
	} else {
		transcript, err = transcribeGoogle(config, apiKey, memo.MimeType, audio)
	}
	if err != nil {
		_, _ = s.database().Exec(`UPDATE audio_memos SET status='failed',error=? WHERE id=?`, err.Error(), id)
		http.Error(w, err.Error(), 502)
		return
	}
	title := "音频备忘 " + strings.ReplaceAll(memo.RecordedDate, "-", "/") + " " + memo.CreatedAt[11:16]
	noteID := "audio-note-" + id
	now := time.Now().Format(time.RFC3339Nano)
	tx, err := s.database().Begin()
	if err == nil {
		var folderID string
		err = tx.QueryRow(`SELECT id FROM folders WHERE notebook_id=? ORDER BY position LIMIT 1`, memo.NotebookID).Scan(&folderID)
		if err == nil {
			content := fmt.Sprintf("---\naliases: [%s]\n---\n# %s\n\n[录音](/audio/%s)\n\n%s\n", id, title, memo.FileName, transcript)
			_, err = tx.Exec(`INSERT INTO documents(id,notebook_id,folder_id,title,content,position,updated_at,created_at,pinned,trashed,private,aliases_json,revision) VALUES(?,?,?,?,?,?,?,?,0,0,0,?,0) ON CONFLICT(id) DO UPDATE SET title=excluded.title,content=excluded.content,updated_at=excluded.updated_at,revision=documents.revision+1`, noteID, memo.NotebookID, folderID, title, content, 999996, now, now, `["`+id+`"]`)
		}
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE audio_memos SET status='done',error='',transcript_document_id=? WHERE id=?`, noteID, id)
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
	memo.Status = "done"
	memo.Error = ""
	memo.TranscriptDocumentID = noteID
	jsonOut(w, memo)
}
