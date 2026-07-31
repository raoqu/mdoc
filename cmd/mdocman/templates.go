package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type noteTemplate struct {
	ID         string `json:"id"`
	NotebookID string `json:"notebookId"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	CreatedAt  string `json:"createdAt"`
}

func (s *server) templates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		notebookID := r.URL.Query().Get("notebookId")
		rows, err := s.database().Query(`SELECT id,notebook_id,title,content,created_at FROM templates WHERE (?='' OR notebook_id=?) ORDER BY lower(title)`, notebookID, notebookID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		items := []noteTemplate{}
		for rows.Next() {
			var item noteTemplate
			if rows.Scan(&item.ID, &item.NotebookID, &item.Title, &item.Content, &item.CreatedAt) == nil {
				items = append(items, item)
			}
		}
		jsonOut(w, items)
	case http.MethodPost:
		var item noteTemplate
		if json.NewDecoder(r.Body).Decode(&item) != nil || strings.TrimSpace(item.NotebookID) == "" || strings.TrimSpace(item.Title) == "" {
			http.Error(w, "notebook and title are required", 400)
			return
		}
		var err error
		item.ID, err = randomToken()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		item.Title = strings.TrimSpace(item.Title)
		item.CreatedAt = time.Now().Format(time.RFC3339Nano)
		_, err = s.database().Exec(`INSERT INTO templates(id,notebook_id,title,content,created_at) VALUES(?,?,?,?,?)`, item.ID, item.NotebookID, item.Title, item.Content, item.CreatedAt)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(item)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *server) template(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/templates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "template id required", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var item noteTemplate
		if json.NewDecoder(r.Body).Decode(&item) != nil || strings.TrimSpace(item.Title) == "" {
			http.Error(w, "title is required", 400)
			return
		}
		result, err := s.database().Exec(`UPDATE templates SET title=?,content=? WHERE id=?`, strings.TrimSpace(item.Title), item.Content, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			http.Error(w, "template not found", 404)
			return
		}
		item.ID = id
		jsonOut(w, item)
	case http.MethodDelete:
		result, err := s.database().Exec(`DELETE FROM templates WHERE id=?`, id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			http.Error(w, "template not found", 404)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", 405)
	}
}
