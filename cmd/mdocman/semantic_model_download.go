package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	semanticModelHuggingFaceURL = "https://huggingface.co/Qdrant/all-MiniLM-L6-v2-onnx"
	semanticModelModelScopeURL  = "https://modelscope.cn/models/sentence-transformers/all-MiniLM-L6-v2"
	semanticModelProbeBytes     = 256 * 1024
)

var (
	errSemanticModelDownloadRunning   = errors.New("模型正在下载")
	errSemanticModelAlreadyInstalled  = errors.New("语义模型已经安装")
	errSemanticModelUnsupportedSource = errors.New("不支持的模型下载源")
)

type semanticModelRemoteFile struct {
	LocalPath  string
	RemotePath string
	Size       int64
	SHA256     string
}

type semanticModelSource struct {
	ID            string
	Label         string
	RepositoryURL string
	Revision      string
	Files         []semanticModelRemoteFile
}

type semanticModelSourceLink struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type semanticModelDownloadStatus struct {
	Status         string                    `json:"status"`
	Source         string                    `json:"source,omitempty"`
	SourceLabel    string                    `json:"sourceLabel,omitempty"`
	Downloaded     int64                     `json:"downloaded"`
	Total          int64                     `json:"total"`
	BytesPerSecond int64                     `json:"bytesPerSecond,omitempty"`
	CurrentFile    string                    `json:"currentFile,omitempty"`
	FileIndex      int                       `json:"fileIndex,omitempty"`
	FileTotal      int                       `json:"fileTotal,omitempty"`
	Target         string                    `json:"target"`
	Message        string                    `json:"message,omitempty"`
	Sources        []semanticModelSourceLink `json:"sources"`
}

type semanticModelDownloader struct {
	mu          sync.RWMutex
	client      *http.Client
	target      string
	sources     []semanticModelSource
	status      semanticModelDownloadStatus
	running     bool
	startedAt   time.Time
	startedByte int64
	retryDelay  time.Duration
}

func defaultSemanticModelSources() []semanticModelSource {
	return []semanticModelSource{
		{
			ID:            "huggingface",
			Label:         "Hugging Face",
			RepositoryURL: semanticModelHuggingFaceURL,
			Revision:      "main",
			Files: []semanticModelRemoteFile{
				{LocalPath: "model.onnx", RemotePath: "model.onnx", Size: 90387630, SHA256: "bbd7b466f6d58e646fdc2bd5fd67b2f5e93c0b687011bd4548c420f7bd46f0c5"},
				{LocalPath: "tokenizer.json", RemotePath: "tokenizer.json", Size: 711661, SHA256: "da0e79933b9ed51798a3ae27893d3c5fa4a201126cef75586296df9b4d2c62a0"},
				{LocalPath: "config.json", RemotePath: "config.json", Size: 650, SHA256: "1b4d8e2a3988377ed8b519a31d8d31025a25f1c5f8606998e8014111438efcd7"},
				{LocalPath: "special_tokens_map.json", RemotePath: "special_tokens_map.json", Size: 695, SHA256: "5d5b662e421ea9fac075174bb0688ee0d9431699900b90662acd44b2a350503a"},
				{LocalPath: "tokenizer_config.json", RemotePath: "tokenizer_config.json", Size: 1433, SHA256: "bd2e06a5b20fd1b13ca988bedc8763d332d242381b4fbc98f8fead4524158f79"},
			},
		},
		{
			ID:            "modelscope",
			Label:         "ModelScope",
			RepositoryURL: semanticModelModelScopeURL,
			Revision:      "master",
			Files: []semanticModelRemoteFile{
				{LocalPath: "model.onnx", RemotePath: "onnx/model.onnx", Size: 90405214, SHA256: "6fd5d72fe4589f189f8ebc006442dbb529bb7ce38f8082112682524616046452"},
				{LocalPath: "tokenizer.json", RemotePath: "tokenizer.json", Size: 466247, SHA256: "be50c3628f2bf5bb5e3a7f17b1f74611b2561a3a27eeab05e5aa30f411572037"},
				{LocalPath: "config.json", RemotePath: "config.json", Size: 612, SHA256: "953f9c0d463486b10a6871cc2fd59f223b2c70184f49815e7efbcab5d8908b41"},
				{LocalPath: "special_tokens_map.json", RemotePath: "special_tokens_map.json", Size: 112, SHA256: "303df45a03609e4ead04bc3dc1536d0ab19b5358db685b6f3da123d05ec200e3"},
				{LocalPath: "tokenizer_config.json", RemotePath: "tokenizer_config.json", Size: 350, SHA256: "acb92769e8195aabd29b7b2137a9e6d6e25c476a4f15aa4355c233426c61576b"},
			},
		},
	}
}

func semanticModelTargetDirectory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(semanticModelEnvironment)); configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录：%w", err)
	}
	return filepath.Join(home, ".mdoc", "models", semanticModelDirectory), nil
}

func newSemanticModelDownloader() *semanticModelDownloader {
	target, err := semanticModelTargetDirectory()
	downloader := newSemanticModelDownloaderWithConfig(target, http.DefaultClient, defaultSemanticModelSources())
	if err != nil {
		downloader.status.Status = "failed"
		downloader.status.Message = err.Error()
		return downloader
	}
	if _, validateErr := validateSemanticModelDirectory(target); validateErr == nil {
		downloader.status.Status = "installed"
	}
	return downloader
}

func newSemanticModelDownloaderWithConfig(target string, client *http.Client, sources []semanticModelSource) *semanticModelDownloader {
	links := make([]semanticModelSourceLink, 0, len(sources))
	for _, source := range sources {
		links = append(links, semanticModelSourceLink{ID: source.ID, Label: source.Label, URL: source.RepositoryURL})
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &semanticModelDownloader{
		client:     client,
		target:     target,
		sources:    append([]semanticModelSource(nil), sources...),
		retryDelay: 2 * time.Second,
		status: semanticModelDownloadStatus{
			Status:  "missing",
			Target:  target,
			Sources: links,
		},
	}
}

func (downloader *semanticModelDownloader) snapshot() semanticModelDownloadStatus {
	downloader.mu.RLock()
	defer downloader.mu.RUnlock()
	status := downloader.status
	status.Sources = append([]semanticModelSourceLink(nil), downloader.status.Sources...)
	if downloader.running && !downloader.startedAt.IsZero() {
		elapsed := time.Since(downloader.startedAt).Seconds()
		if elapsed > 0 {
			status.BytesPerSecond = int64(float64(max(status.Downloaded-downloader.startedByte, 0)) / elapsed)
		}
	}
	return status
}

func (downloader *semanticModelDownloader) start(requestedSource string, completed func(string, error)) error {
	requestedSource = strings.ToLower(strings.TrimSpace(requestedSource))
	if requestedSource == "" {
		requestedSource = "auto"
	}
	if requestedSource != "auto" && downloader.findSource(requestedSource) == nil {
		return errSemanticModelUnsupportedSource
	}
	if _, err := validateSemanticModelDirectory(downloader.target); err == nil {
		return errSemanticModelAlreadyInstalled
	}

	downloader.mu.Lock()
	if downloader.running {
		downloader.mu.Unlock()
		return errSemanticModelDownloadRunning
	}
	downloader.running = true
	downloader.startedAt = time.Now()
	downloader.startedByte = 0
	downloader.status.Status = "probing"
	downloader.status.Source = ""
	downloader.status.SourceLabel = ""
	downloader.status.Downloaded = 0
	downloader.status.Total = 0
	downloader.status.BytesPerSecond = 0
	downloader.status.CurrentFile = ""
	downloader.status.FileIndex = 0
	downloader.status.FileTotal = 0
	downloader.status.Message = "正在探测 Hugging Face 与 ModelScope…"
	downloader.mu.Unlock()

	go func() {
		directory, err := downloader.run(requestedSource)
		downloader.mu.Lock()
		downloader.running = false
		if err != nil {
			downloader.status.Status = "failed"
			downloader.status.Message = err.Error()
			downloader.status.BytesPerSecond = 0
		} else {
			downloader.status.Status = "installed"
			downloader.status.Downloaded = downloader.status.Total
			downloader.status.CurrentFile = ""
			downloader.status.Message = ""
			downloader.status.BytesPerSecond = 0
		}
		downloader.mu.Unlock()
		if completed != nil {
			completed(directory, err)
		}
	}()
	return nil
}

func (downloader *semanticModelDownloader) run(requestedSource string) (string, error) {
	source, err := downloader.selectSource(requestedSource)
	if err != nil {
		return "", err
	}
	total := int64(0)
	for _, file := range source.Files {
		total += file.Size
	}
	downloader.mu.Lock()
	downloader.status.Status = "downloading"
	downloader.status.Source = source.ID
	downloader.status.SourceLabel = source.Label
	downloader.status.Total = total
	downloader.status.FileTotal = len(source.Files)
	downloader.status.Message = ""
	downloader.startedAt = time.Now()
	downloader.startedByte = 0
	downloader.mu.Unlock()

	if err = os.MkdirAll(downloader.target, 0700); err != nil {
		return "", fmt.Errorf("创建模型目录：%w", err)
	}
	if err = downloader.writeSourceState(source.ID); err != nil {
		return "", err
	}
	completedBytes := int64(0)
	for index, file := range source.Files {
		downloader.updateFileProgress(file.LocalPath, index+1, completedBytes, 0)
		if err = downloader.downloadFileWithRetry(*source, file, completedBytes); err != nil {
			return "", err
		}
		completedBytes += file.Size
		downloader.updateFileProgress(file.LocalPath, index+1, completedBytes, 0)
	}

	downloader.mu.Lock()
	downloader.status.Status = "verifying"
	downloader.status.Downloaded = completedBytes
	downloader.status.Message = "正在校验模型文件…"
	downloader.mu.Unlock()
	if _, err = validateSemanticModelDirectory(downloader.target); err != nil {
		return "", err
	}
	return downloader.target, nil
}

func (downloader *semanticModelDownloader) selectSource(requested string) (*semanticModelSource, error) {
	if requested != "auto" {
		return downloader.findSource(requested), nil
	}
	if previous := downloader.readSourceState(); previous != "" && downloader.hasPartialFiles() {
		if source := downloader.findSource(previous); source != nil {
			return source, nil
		}
	}
	type probeResult struct {
		source *semanticModelSource
		speed  float64
		err    error
	}
	results := make(chan probeResult, len(downloader.sources))
	for index := range downloader.sources {
		source := &downloader.sources[index]
		go func() {
			speed, err := downloader.probe(*source)
			results <- probeResult{source: source, speed: speed, err: err}
		}()
	}
	var selected *semanticModelSource
	bestSpeed := float64(-1)
	errorsBySource := []string{}
	for range downloader.sources {
		result := <-results
		if result.err != nil {
			errorsBySource = append(errorsBySource, result.source.Label+": "+result.err.Error())
			continue
		}
		if result.speed > bestSpeed {
			selected = result.source
			bestSpeed = result.speed
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("两个模型源都不可用：%s", strings.Join(errorsBySource, "; "))
	}
	return selected, nil
}

func (downloader *semanticModelDownloader) probe(source semanticModelSource) (float64, error) {
	if len(source.Files) == 0 {
		return 0, errors.New("没有可下载文件")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, semanticModelFileURL(source, source.Files[0]), nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", semanticModelProbeBytes-1))
	downloader.setAuthorization(request, source.ID)
	started := time.Now()
	response, err := downloader.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	read, err := io.Copy(io.Discard, io.LimitReader(response.Body, semanticModelProbeBytes))
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(started).Seconds()
	if read == 0 || elapsed <= 0 {
		return 0, errors.New("测速没有收到数据")
	}
	return float64(read) / elapsed, nil
}

func (downloader *semanticModelDownloader) downloadFileWithRetry(source semanticModelSource, file semanticModelRemoteFile, completedBytes int64) error {
	var err error
	for attempt := 0; attempt <= 5; attempt++ {
		if attempt > 0 {
			time.Sleep(downloader.retryDelay)
		}
		if err = downloader.downloadFile(source, file, completedBytes); err == nil {
			return nil
		}
	}
	return fmt.Errorf("下载 %s 失败：%w", file.LocalPath, err)
}

func (downloader *semanticModelDownloader) downloadFile(source semanticModelSource, file semanticModelRemoteFile, completedBytes int64) error {
	finalPath := filepath.Join(downloader.target, filepath.Clean(file.LocalPath))
	partPath := finalPath + ".part"
	if filepath.Dir(finalPath) != downloader.target {
		return fmt.Errorf("不安全的模型文件路径：%s", file.LocalPath)
	}
	if info, err := os.Stat(finalPath); err == nil && info.Mode().IsRegular() && info.Size() == file.Size {
		if file.SHA256 == "" {
			downloader.updateFileProgress(file.LocalPath, 0, completedBytes+file.Size, 0)
			return nil
		}
		actual, hashErr := sha256FileForDownload(finalPath)
		if hashErr == nil && actual == strings.ToLower(file.SHA256) {
			downloader.updateFileProgress(file.LocalPath, 0, completedBytes+file.Size, 0)
			return nil
		}
		if removeErr := os.Remove(finalPath); removeErr != nil {
			return removeErr
		}
	}
	if info, err := os.Stat(finalPath); err == nil && info.Mode().IsRegular() && info.Size() < file.Size {
		if _, partErr := os.Stat(partPath); errors.Is(partErr, os.ErrNotExist) {
			if renameErr := os.Rename(finalPath, partPath); renameErr != nil {
				return renameErr
			}
		} else if removeErr := os.Remove(finalPath); removeErr != nil {
			return removeErr
		}
	} else if err == nil {
		if removeErr := os.Remove(finalPath); removeErr != nil {
			return removeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	offset := int64(0)
	if info, err := os.Stat(partPath); err == nil {
		offset = info.Size()
		if offset > file.Size {
			if removeErr := os.Remove(partPath); removeErr != nil {
				return removeErr
			}
			offset = 0
		}
	}
	if offset == file.Size {
		return downloader.finishPartFile(partPath, finalPath, file)
	}

	request, err := http.NewRequest(http.MethodGet, semanticModelFileURL(source, file), nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	downloader.setAuthorization(request, source.ID)
	response, err := downloader.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 && response.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		offset = 0
	}
	out, err := os.OpenFile(partPath, flags, 0600)
	if err != nil {
		return err
	}
	buffer := make([]byte, 128*1024)
	written := offset
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, err = out.Write(buffer[:count]); err != nil {
				_ = out.Close()
				return err
			}
			written += int64(count)
			downloader.updateFileProgress(file.LocalPath, 0, completedBytes+written, 0)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = out.Close()
			return readErr
		}
	}
	if err = out.Close(); err != nil {
		return err
	}
	if written != file.Size {
		return fmt.Errorf("文件大小不匹配：得到 %d，预期 %d", written, file.Size)
	}
	return downloader.finishPartFile(partPath, finalPath, file)
}

func (downloader *semanticModelDownloader) finishPartFile(partPath, finalPath string, file semanticModelRemoteFile) error {
	if file.SHA256 != "" {
		actual, err := sha256FileForDownload(partPath)
		if err != nil {
			return err
		}
		if actual != strings.ToLower(file.SHA256) {
			_ = os.Remove(partPath)
			return fmt.Errorf("SHA256 不匹配：得到 %s，预期 %s", actual, file.SHA256)
		}
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return err
	}
	return nil
}

func (downloader *semanticModelDownloader) updateFileProgress(file string, index int, downloaded int64, total int64) {
	downloader.mu.Lock()
	defer downloader.mu.Unlock()
	downloader.status.CurrentFile = file
	if index > 0 {
		downloader.status.FileIndex = index
	}
	if downloaded >= 0 {
		downloader.status.Downloaded = min(downloaded, downloader.status.Total)
	}
	if total > 0 {
		downloader.status.Total = total
	}
}

func (downloader *semanticModelDownloader) findSource(id string) *semanticModelSource {
	for index := range downloader.sources {
		if downloader.sources[index].ID == id {
			return &downloader.sources[index]
		}
	}
	return nil
}

func (downloader *semanticModelDownloader) setAuthorization(request *http.Request, source string) {
	token := ""
	if source == "huggingface" {
		token = strings.TrimSpace(os.Getenv("HF_TOKEN"))
		if token == "" {
			token = strings.TrimSpace(os.Getenv("HUGGINGFACE_TOKEN"))
		}
	} else if source == "modelscope" {
		token = strings.TrimSpace(os.Getenv("MODELSCOPE_SDK_TOKEN"))
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("User-Agent", "mdoc-getmodel/1.0")
}

func (downloader *semanticModelDownloader) sourceStatePath() string {
	return filepath.Join(downloader.target, ".getmodel", "source")
}

func (downloader *semanticModelDownloader) writeSourceState(source string) error {
	directory := filepath.Dir(downloader.sourceStatePath())
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary := downloader.sourceStatePath() + ".tmp"
	if err := os.WriteFile(temporary, []byte(source+"\n"), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, downloader.sourceStatePath())
}

func (downloader *semanticModelDownloader) readSourceState() string {
	contents, err := os.ReadFile(downloader.sourceStatePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func (downloader *semanticModelDownloader) hasPartialFiles() bool {
	for _, name := range semanticModelFiles {
		if _, err := os.Stat(filepath.Join(downloader.target, name+".part")); err == nil {
			return true
		}
	}
	return false
}

func semanticModelFileURL(source semanticModelSource, file semanticModelRemoteFile) string {
	base := strings.TrimRight(source.RepositoryURL, "/")
	path := strings.TrimLeft(file.RemotePath, "/")
	if source.ID == "modelscope" {
		return base + "/resolve/" + source.Revision + "/" + path
	}
	return base + "/resolve/" + source.Revision + "/" + path
}

func sha256FileForDownload(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, input); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
