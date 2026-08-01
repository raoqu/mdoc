//go:build !darwin || !cgo

package main

func newSystemSemanticEmbedder() semanticEmbedder {
	return unavailableSemanticEmbedder{}
}
