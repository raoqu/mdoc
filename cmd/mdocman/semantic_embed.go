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

type unavailableSemanticEmbedder struct{}

func (unavailableSemanticEmbedder) Available() bool {
	return false
}

func (unavailableSemanticEmbedder) DisplayName() string {
	return ""
}

func (unavailableSemanticEmbedder) Embed(string) (semanticEmbedding, error) {
	return semanticEmbedding{}, errors.New("local sentence embeddings are unavailable on this platform")
}
