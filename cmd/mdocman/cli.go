package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func runCLI(s *server, args []string, out, errOut io.Writer) bool {
	if len(args) == 0 || args[0] == "serve" {
		return false
	}
	command := args[0]
	args = args[1:]
	switch command {
	case "today":
		date := time.Now().Format("2006-01-02")
		var title, content string
		err := s.database().QueryRow(`SELECT title,content FROM documents WHERE id=? AND trashed=0`, "daily-"+date).Scan(&title, &content)
		if err == sql.ErrNoRows {
			fmt.Fprintf(errOut, "No daily note exists for %s. Open the app to create it.\n", date)
			return true
		}
		if err != nil {
			fmt.Fprintln(errOut, err)
			return true
		}
		fmt.Fprint(out, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(out)
		}
		_ = title
		return true
	case "search":
		query := strings.TrimSpace(strings.Join(args, " "))
		if query == "" {
			fmt.Fprintln(errOut, "Usage: mdocman search <query>")
			return true
		}
		rows, err := s.database().Query(`SELECT d.id,d.title,snippet(documents_fts,2,'','',' … ',12) FROM documents_fts JOIN documents d ON d.id=documents_fts.document_id WHERE documents_fts MATCH ? AND d.trashed=0 ORDER BY bm25(documents_fts) LIMIT 20`, ftsExpression(query))
		if err != nil {
			fmt.Fprintln(errOut, err)
			return true
		}
		defer rows.Close()
		for rows.Next() {
			var id, title, snippet string
			if rows.Scan(&id, &title, &snippet) == nil {
				fmt.Fprintf(out, "%s\t%s\t%s\n", id, title, strings.ReplaceAll(snippet, "\n", " "))
			}
		}
		return true
	case "show", "path":
		ref := strings.TrimSpace(strings.Join(args, " "))
		if ref == "" {
			fmt.Fprintf(errOut, "Usage: mdocman %s <id-or-title>\n", command)
			return true
		}
		var id, notebookID, folderID, title, content string
		err := s.database().QueryRow(`SELECT id,notebook_id,COALESCE(folder_id,''),title,content FROM documents WHERE trashed=0 AND (id=? OR lower(title)=lower(?)) ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END LIMIT 1`, ref, ref, ref).Scan(&id, &notebookID, &folderID, &title, &content)
		if err == sql.ErrNoRows {
			fmt.Fprintf(errOut, "Note not found: %s\n", ref)
			return true
		}
		if err != nil {
			fmt.Fprintln(errOut, err)
			return true
		}
		if command == "path" {
			fmt.Fprintf(out, "%s/%s/%s.md\n", notebookID, folderID, id)
		} else {
			fmt.Fprint(out, content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Fprintln(out)
			}
		}
		_ = title
		return true
	case "help", "--help", "-h":
		fmt.Fprintln(out, "mdoc <路径>")
		fmt.Fprintln(out, "mdocman [serve|today|search <query>|show <id-or-title>|path <id-or-title>]")
		return true
	default:
		fmt.Fprintf(errOut, "Unknown command %q. Run mdocman help.\n", command)
		return true
	}
}

func cliRequested(s *server) bool {
	return runCLI(s, os.Args[1:], os.Stdout, os.Stderr)
}
