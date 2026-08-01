package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeSemanticEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (embedder *fakeSemanticEmbedder) Available() bool {
	return true
}

func (embedder *fakeSemanticEmbedder) DisplayName() string {
	return "Test sentence model"
}

func (embedder *fakeSemanticEmbedder) Embed(text string) (semanticEmbedding, error) {
	embedder.mu.Lock()
	embedder.calls++
	embedder.mu.Unlock()
	folded := strings.ToLower(text)
	values := []float32{0, 0, 1}
	switch {
	case strings.Contains(folded, "automobile"),
		strings.Contains(folded, "vehicle"),
		strings.Contains(folded, "car"):
		values = []float32{1, 0, 0}
	case strings.Contains(folded, "banana"), strings.Contains(folded, "fruit"):
		values = []float32{0, 1, 0}
	}
	return semanticEmbedding{
		ModelID:  "test/en/rev1",
		Language: "en",
		Values:   values,
	}, nil
}

func (embedder *fakeSemanticEmbedder) callCount() int {
	embedder.mu.Lock()
	defer embedder.mu.Unlock()
	return embedder.calls
}

func readySemanticTestServer(t *testing.T) (*server, *fakeSemanticEmbedder, func()) {
	t.Helper()
	instance, closeServer := chatToolTestServer(t)
	embedder := &fakeSemanticEmbedder{}
	runtime := newSemanticServiceWithEmbedder(embedder)
	runtime.configure(true)
	instance.semantic = runtime
	indexed, total, err := runtime.rebuild(instance.database(), instance.uploadsDir(), false)
	if err != nil {
		closeServer()
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.status.Status = "ready"
	runtime.status.Indexed = indexed
	runtime.status.Total = total
	runtime.readyDB = instance.database()
	runtime.mu.Unlock()
	return instance, embedder, closeServer
}

func TestSemanticHybridSearchFindsMeaningAndExcludesPrivateNotes(t *testing.T) {
	instance, embedder, closeServer := readySemanticTestServer(t)
	defer closeServer()
	putChatToolDocument(t, instance, "public-car", "Garage checklist", "An automobile needs regular maintenance before a long journey.", false)
	putChatToolDocument(t, instance, "fruit", "Fruit notes", "Bananas ripen quickly in a paper bag.", false)
	putChatToolDocument(t, instance, "secret-car", "Secret garage", "---\nprivate: true\n---\nA vehicle upkeep schedule nobody may read.", false)

	indexed, total, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	instance.semantic.mu.Lock()
	instance.semantic.status.Status = "ready"
	instance.semantic.status.Indexed = indexed
	instance.semantic.status.Total = total
	instance.semantic.readyDB = instance.database()
	instance.semantic.mu.Unlock()

	result := instance.executeChatTool("book", aiToolCall{
		ID:        "semantic-search",
		Name:      "search_notes",
		Arguments: `{"query":"vehicle upkeep","limit":10}`,
	}, true)
	if result.Activity.Status != "complete" {
		t.Fatalf("activity = %#v", result.Activity)
	}
	var output struct {
		Mode string `json:"mode"`
		Hits []struct {
			DocumentID string `json:"documentId"`
		} `json:"hits"`
	}
	if err = json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatal(err)
	}
	if output.Mode != "hybrid" {
		t.Fatalf("mode = %q, output = %s", output.Mode, result.Output)
	}
	foundPublic := false
	for _, hit := range output.Hits {
		if hit.DocumentID == "public-car" {
			foundPublic = true
		}
		if hit.DocumentID == "secret-car" {
			t.Fatalf("private semantic hit leaked: %s", result.Output)
		}
	}
	if !foundPublic {
		t.Fatalf("meaning-level result missing: %s", result.Output)
	}
	var privateChunks int
	if err = instance.database().QueryRow(`SELECT COUNT(*) FROM semantic_chunks WHERE document_id='secret-car'`).Scan(&privateChunks); err != nil {
		t.Fatal(err)
	}
	if privateChunks != 0 {
		t.Fatalf("private note produced %d semantic chunks", privateChunks)
	}
	if embedder.callCount() == 0 {
		t.Fatal("fake sentence model was never used")
	}
}

func TestGlobalSearchIncludesSemanticOnlyMatches(t *testing.T) {
	instance, _, closeServer := readySemanticTestServer(t)
	defer closeServer()
	putChatToolDocument(t, instance, "public-car", "Garage checklist", "An automobile needs regular maintenance before a long journey.", false)
	putChatToolDocument(t, instance, "fruit", "Fruit notes", "Bananas ripen quickly in a paper bag.", false)
	putChatToolDocument(t, instance, "secret-car", "Secret garage", "---\nprivate: true\n---\nA confidential automobile schedule nobody may read.", false)

	indexed, total, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	instance.semantic.mu.Lock()
	instance.semantic.status.Status = "ready"
	instance.semantic.status.Indexed = indexed
	instance.semantic.status.Total = total
	instance.semantic.readyDB = instance.database()
	instance.semantic.mu.Unlock()

	request := httptest.NewRequest(http.MethodGet, "/api/search?q=vehicle+upkeep&notebookId=book", nil)
	response := httptest.NewRecorder()
	instance.search(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var hits []searchHit
	if err = json.Unmarshal(response.Body.Bytes(), &hits); err != nil {
		t.Fatal(err)
	}
	foundPublic := false
	for _, hit := range hits {
		if hit.ID == "public-car" {
			foundPublic = true
		}
		if hit.ID == "secret-car" {
			t.Fatalf("private semantic hit leaked: %s", response.Body.String())
		}
	}
	if !foundPublic {
		t.Fatalf("semantic-only match missing: %s", response.Body.String())
	}
}

func TestFuseSearchResultsRewardsAgreementAndKeepsLexicalTiePriority(t *testing.T) {
	lexical := []searchHit{{ID: "exact", Title: "Exact"}, {ID: "both", Title: "Both"}}
	semantic := []searchHit{{ID: "meaning", Title: "Meaning"}, {ID: "both", Title: "Both"}}
	fused := fuseSearchResults([][]searchHit{lexical, semantic}, 10)
	if len(fused) != 3 {
		t.Fatalf("fused = %#v", fused)
	}
	if fused[0].ID != "both" {
		t.Fatalf("agreement should rank first: %#v", fused)
	}
	if fused[1].ID != "exact" || fused[2].ID != "meaning" {
		t.Fatalf("lexical tie should win: %#v", fused)
	}
}

func TestSemanticSimilarNotesExcludesSourceUnrelatedAndPrivateNotes(t *testing.T) {
	instance, _, closeServer := readySemanticTestServer(t)
	defer closeServer()
	putChatToolDocument(t, instance, "car-source", "Car source", "An automobile maintenance checklist.", false)
	putChatToolDocument(t, instance, "car-related", "Road trip", "A vehicle needs servicing before a journey.", false)
	putChatToolDocument(t, instance, "fruit", "Fruit", "Bananas ripen in a paper bag.", false)
	putChatToolDocument(t, instance, "car-private", "Private garage", "A private car repair log.", true)

	indexed, total, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	instance.semantic.mu.Lock()
	instance.semantic.status.Status = "ready"
	instance.semantic.status.Indexed = indexed
	instance.semantic.status.Total = total
	instance.semantic.readyDB = instance.database()
	instance.semantic.mu.Unlock()

	hits, err := instance.semantic.similar(instance.database(), "book", "car-source", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "car-related" {
		t.Fatalf("similar hits = %#v", hits)
	}
}

func TestSemanticSimilarEndpointReportsNotReady(t *testing.T) {
	instance, closeServer := chatToolTestServer(t)
	defer closeServer()
	instance.semantic = newSemanticServiceWithEmbedder(&fakeSemanticEmbedder{})
	request := httptest.NewRequest(http.MethodGet, "/api/semantic/similar?documentId=one&notebookId=book", nil)
	response := httptest.NewRecorder()
	instance.semanticSimilar(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSemanticRebuildSkipsUnchangedDocumentHashes(t *testing.T) {
	instance, embedder, closeServer := readySemanticTestServer(t)
	defer closeServer()
	putChatToolDocument(t, instance, "car", "Car note", "An automobile maintenance checklist.", false)

	if _, _, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false); err != nil {
		t.Fatal(err)
	}
	afterFirst := embedder.callCount()
	if _, _, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false); err != nil {
		t.Fatal(err)
	}
	if afterSecond := embedder.callCount(); afterSecond != afterFirst {
		t.Fatalf("unchanged rebuild embedded again: first=%d second=%d", afterFirst, afterSecond)
	}
	if _, err := instance.database().Exec(`UPDATE documents SET content='An automobile maintenance checklist with tire pressure.' WHERE id='car'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := instance.semantic.rebuild(instance.database(), instance.uploadsDir(), false); err != nil {
		t.Fatal(err)
	}
	if afterChange := embedder.callCount(); afterChange <= afterFirst {
		t.Fatalf("changed document was not re-embedded: before=%d after=%d", afterFirst, afterChange)
	}
}

func TestSemanticChunkingKeepsHeadingsAndMergesSmallTail(t *testing.T) {
	body := "# Alpha\n\n" + strings.Repeat("One useful sentence. ", 70) + "\n\nShort tail."
	chunks := semanticChunks(body, nil)
	if len(chunks) < 1 {
		t.Fatal("expected at least one semantic chunk")
	}
	for _, chunk := range chunks {
		if chunk.Heading != "Alpha" {
			t.Fatalf("heading = %q", chunk.Heading)
		}
		if strings.TrimSpace(chunk.Text) == "" || chunk.To <= chunk.From {
			t.Fatalf("invalid chunk = %#v", chunk)
		}
	}
	if !strings.Contains(chunks[len(chunks)-1].Text, "Short tail.") {
		t.Fatalf("small tail was lost: %#v", chunks[len(chunks)-1])
	}
}

func TestPlatformSentenceEmbeddingIsNormalizedWhenAvailable(t *testing.T) {
	embedder := newPlatformSemanticEmbedder()
	if !embedder.Available() {
		t.Skip("platform sentence embeddings unavailable")
	}
	for _, text := range []string{"Where is my order?", "我的订单在哪里？"} {
		vector, err := embedder.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		if vector.ModelID == "" || vector.Language == "" || len(vector.Values) == 0 {
			t.Fatalf("embedding metadata = %#v", vector)
		}
		var norm float64
		for _, value := range vector.Values {
			norm += float64(value) * float64(value)
		}
		if math.Abs(norm-1) > 0.0001 {
			t.Fatalf("vector norm = %f", norm)
		}
	}
}

func TestMiniLMModelProducesUseful384DimensionEmbeddings(t *testing.T) {
	directory, err := locateSemanticModelDirectory()
	if err != nil {
		t.Skip(err)
	}
	embedder := &modelSemanticEmbedder{directory: directory}
	embeddings, err := embedder.EmbedBatch([]string{
		"How do I repair a bicycle tire?",
		"Steps for fixing a flat bike wheel",
		"A recipe for chocolate cake",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddings) != 3 {
		t.Fatalf("embeddings = %d", len(embeddings))
	}
	for _, embedding := range embeddings {
		if embedding.ModelID != semanticModelID || embedding.Language != "und" || len(embedding.Values) != semanticModelDimensions {
			t.Fatalf("embedding metadata = %#v", embedding)
		}
	}
	similar := semanticDot(embeddings[0].Values, embeddings[1].Values)
	unrelated := semanticDot(embeddings[0].Values, embeddings[2].Values)
	if similar <= unrelated+0.2 {
		t.Fatalf("similarity separation too small: similar=%f unrelated=%f", similar, unrelated)
	}
}

func TestPlatformSemanticIndexRetrievesAParaphraseWhenAvailable(t *testing.T) {
	embedder := newPlatformSemanticEmbedder()
	if !embedder.Available() {
		t.Skip("platform sentence embeddings unavailable")
	}
	instance, closeServer := chatToolTestServer(t)
	defer closeServer()
	runtime := newSemanticServiceWithEmbedder(embedder)
	runtime.configure(true)
	instance.semantic = runtime
	putChatToolDocument(t, instance, "delivery", "Delivery FAQ", "How do I check the status of my order?", false)
	putChatToolDocument(t, instance, "banana", "Fruit", "Bananas are yellow fruit.", false)
	indexed, total, err := runtime.rebuild(instance.database(), instance.uploadsDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.status.Status = "ready"
	runtime.status.Indexed = indexed
	runtime.status.Total = total
	runtime.readyDB = instance.database()
	runtime.mu.Unlock()

	result := instance.executeChatTool("book", aiToolCall{
		ID:        "native-semantic-search",
		Name:      "search_notes",
		Arguments: `{"query":"How can I follow my shipment?","limit":5}`,
	}, true)
	if !strings.Contains(result.Output, `"documentId":"delivery"`) {
		t.Fatalf("native sentence model missed paraphrase: %s", result.Output)
	}
	if strings.Contains(result.Output, `"documentId":"banana"`) {
		t.Fatalf("native sentence model returned unrelated note: %s", result.Output)
	}
}
