package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	semanticModelID          = "all-MiniLM-L6-v2"
	semanticModelDirectory   = "Qdrant-all-MiniLM-L6-v2-onnx"
	semanticModelEnvironment = "MDOC_SEMANTIC_MODEL_DIR"
	semanticModelDimensions  = 384
)

var semanticModelFiles = []string{
	"model.onnx",
	"tokenizer.json",
	"config.json",
	"special_tokens_map.json",
	"tokenizer_config.json",
}

type modelSemanticEmbedder struct {
	directory string
	once      sync.Once
	session   *hugot.Session
	pipeline  *pipelines.FeatureExtractionPipeline
	loadErr   error
}

func newPlatformSemanticEmbedder() semanticEmbedder {
	directory, err := locateSemanticModelDirectory()
	if err == nil {
		return &modelSemanticEmbedder{directory: directory}
	}
	return unavailableSemanticEmbedder{message: err.Error()}
}

func (embedder *modelSemanticEmbedder) Available() bool {
	return embedder != nil && embedder.directory != ""
}

func (embedder *modelSemanticEmbedder) DisplayName() string {
	return semanticModelID + " (ONNX)"
}

func (embedder *modelSemanticEmbedder) Embed(text string) (semanticEmbedding, error) {
	embeddings, err := embedder.EmbedBatch([]string{text})
	if err != nil {
		return semanticEmbedding{}, err
	}
	return embeddings[0], nil
}

func (embedder *modelSemanticEmbedder) EmbedBatch(texts []string) ([]semanticEmbedding, error) {
	inputs := make([]string, len(texts))
	for index, text := range texts {
		inputs[index] = strings.TrimSpace(text)
		if inputs[index] == "" {
			return nil, errors.New("cannot embed empty text")
		}
	}
	if len(inputs) == 0 {
		return []semanticEmbedding{}, nil
	}
	embedder.once.Do(embedder.load)
	if embedder.loadErr != nil {
		return nil, embedder.loadErr
	}
	output, err := embedder.pipeline.RunPipeline(inputs)
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", semanticModelID, err)
	}
	if len(output.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("%s returned %d embeddings for %d inputs", semanticModelID, len(output.Embeddings), len(inputs))
	}
	embeddings := make([]semanticEmbedding, len(output.Embeddings))
	for index, outputValues := range output.Embeddings {
		if len(outputValues) != semanticModelDimensions {
			return nil, fmt.Errorf(
				"%s returned %d dimensions for input %d",
				semanticModelID,
				len(outputValues),
				index,
			)
		}
		values := append([]float32(nil), outputValues...)
		for _, value := range values {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, fmt.Errorf("%s returned a non-finite vector", semanticModelID)
			}
		}
		embeddings[index] = semanticEmbedding{
			ModelID:  semanticModelID,
			Language: "und",
			Values:   values,
		}
	}
	return embeddings, nil
}

func (embedder *modelSemanticEmbedder) load() {
	session, err := hugot.NewGoSession()
	if err != nil {
		embedder.loadErr = fmt.Errorf("initialize the local ONNX runtime: %w", err)
		return
	}
	pipeline, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath: embedder.directory,
		Name:      "mdoc-semantic-search",
		Options: []hugot.FeatureExtractionOption{
			pipelines.WithNormalization(),
		},
	})
	if err != nil {
		_ = session.Destroy()
		embedder.loadErr = fmt.Errorf("load %s from %s: %w", semanticModelID, embedder.directory, err)
		return
	}
	embedder.session = session
	embedder.pipeline = pipeline
}

func locateSemanticModelDirectory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(semanticModelEnvironment)); configured != "" {
		return validateSemanticModelDirectory(configured)
	}
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".mdoc", "models", semanticModelDirectory))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		for directory := workingDirectory; ; directory = filepath.Dir(directory) {
			candidates = append(candidates,
				filepath.Join(directory, "models", semanticModelDirectory),
				filepath.Join(directory, "_models", "models", semanticModelDirectory),
			)
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	for _, candidate := range candidates {
		if directory, err := validateSemanticModelDirectory(candidate); err == nil {
			return directory, nil
		}
	}
	return "", fmt.Errorf(
		"未找到 %s 模型；请把完整模型目录放到 ~/.mdoc/models/%s，或设置 %s",
		semanticModelID,
		semanticModelDirectory,
		semanticModelEnvironment,
	)
}

func validateSemanticModelDirectory(candidate string) (string, error) {
	directory, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(filepath.Join(directory, "model.onnx")); statErr != nil && errors.Is(statErr, os.ErrNotExist) {
		nested := filepath.Join(directory, semanticModelDirectory)
		if _, nestedErr := os.Stat(filepath.Join(nested, "model.onnx")); nestedErr == nil {
			directory = nested
		}
	} else if statErr != nil {
		return "", statErr
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s/model.onnx is not a regular file", directory)
	}
	missing := []string{}
	for _, name := range semanticModelFiles {
		info, statErr := os.Stat(filepath.Join(directory, name))
		if statErr != nil || !info.Mode().IsRegular() {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("模型目录 %s 缺少文件：%s", directory, strings.Join(missing, ", "))
	}
	return directory, nil
}
