#ifndef MDOCMAN_SEMANTIC_NL_DARWIN_H
#define MDOCMAN_SEMANTIC_NL_DARWIN_H

#include <stdlib.h>

typedef struct {
  double *values;
  int length;
  int revision;
  char *language;
  char *error_message;
} MdocEmbeddingResult;

MdocEmbeddingResult mdoc_embed_sentence(const char *input);
void mdoc_free_embedding(MdocEmbeddingResult result);

#endif
