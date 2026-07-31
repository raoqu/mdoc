//go:build !darwin || !cgo

package main

func newPlatformSemanticEmbedder() semanticEmbedder {
	return unavailableSemanticEmbedder{}
}
