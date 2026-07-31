//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation -framework NaturalLanguage
#include "semantic_nl_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unsafe"
)

type appleNaturalLanguageEmbedder struct{}

func newPlatformSemanticEmbedder() semanticEmbedder {
	return appleNaturalLanguageEmbedder{}
}

func (appleNaturalLanguageEmbedder) Available() bool {
	return true
}

func (appleNaturalLanguageEmbedder) DisplayName() string {
	return "Apple Natural Language"
}

func (appleNaturalLanguageEmbedder) Embed(text string) (semanticEmbedding, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return semanticEmbedding{}, errors.New("cannot embed empty text")
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	result := C.mdoc_embed_sentence(cText)
	defer C.mdoc_free_embedding(result)
	if result.error_message != nil {
		return semanticEmbedding{}, errors.New(C.GoString(result.error_message))
	}
	if result.values == nil || result.length <= 0 || result.language == nil {
		return semanticEmbedding{}, errors.New("the system sentence model returned no vector")
	}
	raw := unsafe.Slice((*C.double)(result.values), int(result.length))
	values := make([]float32, len(raw))
	var norm float64
	for index, value := range raw {
		norm += float64(value) * float64(value)
		values[index] = float32(value)
	}
	if norm == 0 {
		return semanticEmbedding{}, errors.New("the system sentence model returned a zero vector")
	}
	scale := float32(1 / math.Sqrt(norm))
	for index := range values {
		values[index] *= scale
	}
	language := C.GoString(result.language)
	return semanticEmbedding{
		ModelID:  fmt.Sprintf("apple-nlembedding/%s/rev%d", language, int(result.revision)),
		Language: language,
		Values:   values,
	}, nil
}
