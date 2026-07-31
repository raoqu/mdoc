#import <Foundation/Foundation.h>
#import <NaturalLanguage/NaturalLanguage.h>

#include "semantic_nl_darwin.h"
#include <string.h>

static MdocEmbeddingResult mdoc_embedding_error(NSString *message) {
  MdocEmbeddingResult result = {0};
  result.error_message = strdup(message.UTF8String);
  return result;
}

static NLLanguage mdoc_embedding_language(NSString *text) {
  NLLanguageRecognizer *recognizer = [[NLLanguageRecognizer alloc] init];
  [recognizer processString:text];
  NLLanguage language = recognizer.dominantLanguage;
  [recognizer release];
  if ([language isEqualToString:NLLanguageTraditionalChinese]) {
    return NLLanguageSimplifiedChinese;
  }
  return language ?: NLLanguageEnglish;
}

MdocEmbeddingResult mdoc_embed_sentence(const char *input) {
  @autoreleasepool {
    if (input == NULL) {
      return mdoc_embedding_error(@"sentence text is missing");
    }
    NSString *text = [NSString stringWithUTF8String:input];
    if (text == nil || text.length == 0) {
      return mdoc_embedding_error(@"sentence text is empty");
    }
    NLLanguage language = mdoc_embedding_language(text);
    NLEmbedding *embedding = [NLEmbedding sentenceEmbeddingForLanguage:language];
    if (embedding == nil && ![language isEqualToString:NLLanguageEnglish]) {
      language = NLLanguageEnglish;
      embedding = [NLEmbedding sentenceEmbeddingForLanguage:language];
    }
    if (embedding == nil) {
      return mdoc_embedding_error(@"no system sentence embedding is available");
    }
    NSArray<NSNumber *> *vector = [embedding vectorForString:text];
    if (vector == nil || vector.count == 0) {
      return mdoc_embedding_error(@"the system sentence model could not embed this text");
    }
    MdocEmbeddingResult result = {0};
    result.length = (int)vector.count;
    result.revision = (int)embedding.revision;
    result.language = strdup(language.UTF8String);
    result.values = calloc(vector.count, sizeof(double));
    if (result.language == NULL || result.values == NULL) {
      mdoc_free_embedding(result);
      return mdoc_embedding_error(@"could not allocate the sentence vector");
    }
    for (NSUInteger index = 0; index < vector.count; index++) {
      result.values[index] = vector[index].doubleValue;
    }
    return result;
  }
}

void mdoc_free_embedding(MdocEmbeddingResult result) {
  free(result.values);
  free(result.language);
  free(result.error_message);
}
