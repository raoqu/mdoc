package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func semanticDownloadFixture() map[string][]byte {
	return map[string][]byte{
		"model.onnx":              bytes.Repeat([]byte("model-data-"), 4096),
		"tokenizer.json":          []byte(`{"tokenizer":true}`),
		"config.json":             []byte(`{"model_type":"bert"}`),
		"special_tokens_map.json": []byte(`{"unk_token":"[UNK]"}`),
		"tokenizer_config.json":   []byte(`{"do_lower_case":true}`),
	}
}

func semanticDownloadSource(id, label, repositoryURL string, fixture map[string][]byte) semanticModelSource {
	files := make([]semanticModelRemoteFile, 0, len(semanticModelFiles))
	for _, name := range semanticModelFiles {
		hash := sha256.Sum256(fixture[name])
		files = append(files, semanticModelRemoteFile{
			LocalPath:  name,
			RemotePath: name,
			Size:       int64(len(fixture[name])),
			SHA256:     hex.EncodeToString(hash[:]),
		})
	}
	return semanticModelSource{ID: id, Label: label, RepositoryURL: repositoryURL, Revision: "main", Files: files}
}

func serveSemanticDownloadFile(w http.ResponseWriter, r *http.Request, contents []byte) {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(contents)
		return
	}
	rangeValue := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(rangeValue, "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 0 || start >= len(contents) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	end := len(contents) - 1
	if len(parts) == 2 && parts[1] != "" {
		if requestedEnd, parseErr := strconv.Atoi(parts[1]); parseErr == nil && requestedEnd < end {
			end = requestedEnd
		}
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(contents)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(contents[start : end+1])
}

func waitForSemanticDownload(t *testing.T, downloader *semanticModelDownloader, completed <-chan error) {
	t.Helper()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("download did not finish; status=%#v", downloader.snapshot())
	}
}

func TestSemanticModelDownloaderDownloadsAndVerifiesFiles(t *testing.T) {
	fixture := semanticDownloadFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		contents, ok := fixture[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		serveSemanticDownloadFile(w, r, contents)
	}))
	defer server.Close()

	target := t.TempDir()
	source := semanticDownloadSource("huggingface", "Hugging Face", server.URL+"/repo", fixture)
	downloader := newSemanticModelDownloaderWithConfig(target, server.Client(), []semanticModelSource{source})
	downloader.retryDelay = time.Millisecond
	completed := make(chan error, 1)
	if err := downloader.start("huggingface", func(_ string, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	waitForSemanticDownload(t, downloader, completed)

	status := downloader.snapshot()
	if status.Status != "installed" || status.Source != "huggingface" || status.Downloaded != status.Total {
		t.Fatalf("status = %#v", status)
	}
	for name, expected := range fixture {
		actual, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("%s was not downloaded intact", name)
		}
		if _, err = os.Stat(filepath.Join(target, name+".part")); !os.IsNotExist(err) {
			t.Fatalf("partial file remains for %s", name)
		}
	}
}

func TestSemanticModelDownloaderResumesPartialFile(t *testing.T) {
	fixture := semanticDownloadFixture()
	var mu sync.Mutex
	modelRanges := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == "model.onnx" {
			mu.Lock()
			modelRanges = append(modelRanges, r.Header.Get("Range"))
			mu.Unlock()
		}
		serveSemanticDownloadFile(w, r, fixture[name])
	}))
	defer server.Close()

	target := t.TempDir()
	partial := fixture["model.onnx"][:777]
	if err := os.WriteFile(filepath.Join(target, "model.onnx.part"), partial, 0600); err != nil {
		t.Fatal(err)
	}
	source := semanticDownloadSource("huggingface", "Hugging Face", server.URL+"/repo", fixture)
	downloader := newSemanticModelDownloaderWithConfig(target, server.Client(), []semanticModelSource{source})
	downloader.retryDelay = time.Millisecond
	completed := make(chan error, 1)
	if err := downloader.start("huggingface", func(_ string, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	waitForSemanticDownload(t, downloader, completed)

	mu.Lock()
	ranges := append([]string(nil), modelRanges...)
	mu.Unlock()
	if len(ranges) == 0 || ranges[0] != "bytes=777-" {
		t.Fatalf("model ranges = %#v", ranges)
	}
}

func TestSemanticModelDownloaderAutoSelectsAvailableSource(t *testing.T) {
	fixture := semanticDownloadFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hf/") {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		serveSemanticDownloadFile(w, r, fixture[filepath.Base(r.URL.Path)])
	}))
	defer server.Close()

	sources := []semanticModelSource{
		semanticDownloadSource("huggingface", "Hugging Face", server.URL+"/hf", fixture),
		semanticDownloadSource("modelscope", "ModelScope", server.URL+"/ms", fixture),
	}
	downloader := newSemanticModelDownloaderWithConfig(t.TempDir(), server.Client(), sources)
	downloader.retryDelay = time.Millisecond
	completed := make(chan error, 1)
	if err := downloader.start("auto", func(_ string, err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	waitForSemanticDownload(t, downloader, completed)
	if status := downloader.snapshot(); status.Source != "modelscope" {
		t.Fatalf("automatic source = %#v", status)
	}
}

func TestSemanticServiceActivatesDownloadedModel(t *testing.T) {
	fixture := semanticDownloadFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveSemanticDownloadFile(w, r, fixture[filepath.Base(r.URL.Path)])
	}))
	defer server.Close()

	target := t.TempDir()
	source := semanticDownloadSource("modelscope", "ModelScope", server.URL+"/ms", fixture)
	downloader := newSemanticModelDownloaderWithConfig(target, server.Client(), []semanticModelSource{source})
	downloader.retryDelay = time.Millisecond
	runtime := newSemanticServiceWithEmbedder(unavailableSemanticEmbedder{message: "missing"})
	runtime.downloader = downloader
	if err := runtime.requestModelDownload("modelscope", true, func() *sql.DB { return nil }, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := runtime.snapshot(nil)
		if status.ModelDownload.Status == "failed" {
			t.Fatal(status.ModelDownload.Message)
		}
		if status.ModelDownload.Status == "installed" && status.Available {
			if !status.Enabled || status.Model != semanticModelID+" (ONNX)" {
				t.Fatalf("activated status = %#v", status)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("model was not activated; status=%#v", runtime.snapshot(nil))
}

func TestSemanticModelDownloadEndpointRejectsUnknownSource(t *testing.T) {
	runtime := newSemanticServiceWithEmbedder(unavailableSemanticEmbedder{message: "missing"})
	runtime.downloader = newSemanticModelDownloaderWithConfig(t.TempDir(), http.DefaultClient, defaultSemanticModelSources())
	instance := &server{semantic: runtime}
	request := httptest.NewRequest(http.MethodPost, "/api/semantic/model", strings.NewReader(`{"source":"unknown","enable":true}`))
	response := httptest.NewRecorder()
	instance.semanticModelDownload(response, request)
	result := response.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(result.Body)
		t.Fatalf("status = %d, body = %s", result.StatusCode, body)
	}
}
