package main

import "errors"

type semanticEmbedding struct {
	ModelID  string
	Language string
	Values   []float32
}

type semanticEmbedder interface {
	Available() bool
	DisplayName() string
	Embed(text string) (semanticEmbedding, error)
}

type semanticBatchEmbedder interface {
	EmbedBatch(texts []string) ([]semanticEmbedding, error)
}

type unavailableSemanticEmbedder struct {
	message string
}

func (unavailableSemanticEmbedder) Available() bool {
	return false
}

func (unavailableSemanticEmbedder) DisplayName() string {
	return ""
}

func (embedder unavailableSemanticEmbedder) Embed(string) (semanticEmbedding, error) {
	if embedder.message != "" {
		return semanticEmbedding{}, errors.New(embedder.message)
	}
	return semanticEmbedding{}, errors.New("local sentence embeddings are unavailable on this platform")
}

func (embedder unavailableSemanticEmbedder) UnavailableReason() string {
	if embedder.message != "" {
		return embedder.message
	}
	return "当前平台没有可用的本地句向量运行时。"
}
