package search_test

import (
	"context"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/store"
)

// chunk builds a searchable chunk with a citable source.
func chunk(id, libraryID, content string, vector [3]float32) store.Chunk {
	return store.Chunk{
		ID:          id,
		SourceID:    "src-" + libraryID,
		LibraryID:   libraryID,
		Version:     versionOf(libraryID),
		DocumentID:  "doc-" + id,
		URL:         "https://docs.example.test/" + id,
		Title:       "Doc " + id,
		HeadingPath: "Guide > Section " + id,
		Index:       0,
		Content:     content,
		Embedding:   vector[:],
	}
}

func versionOf(libraryID string) string {
	parts := strings.Split(libraryID, "/")
	if len(parts) > 3 {
		return parts[3]
	}
	return ""
}

// newTestEngine wires a search engine onto an in-memory store, so retrieval is
// testable without Chroma or a model.
func newTestEngine(t *testing.T, chunks ...store.Chunk) *search.Engine {
	t.Helper()

	mem := newMemoryStore(chunks...)

	engine, err := search.NewEngine(mem, &fixedEmbedder{},
		search.Options{CandidateCount: 20, FinalChunks: 6})
	if err != nil {
		t.Fatalf("search.NewEngine: %v", err)
	}
	return engine
}

// fixedEmbedder maps every query onto the same vector the auth fixtures use, so
// retrieval tests assert ordering rather than model behaviour.
type fixedEmbedder struct {
	vector []float32
}

func (f *fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vector := f.vector
	if vector == nil {
		vector = []float32{0, 1, 0}
	}

	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = vector
	}
	return out, nil
}

func (f *fixedEmbedder) Provider() string { return "fake" }
func (f *fixedEmbedder) Model() string    { return "fake-model" }
func (f *fixedEmbedder) Dimensions() int  { return 3 }

// newMemoryStore is a minimal in-process Store: enough to exercise ranking
// without Chroma.
type memoryStore struct {
	chunks map[string]store.Chunk
}

func newMemoryStore(chunks ...store.Chunk) *memoryStore {
	m := &memoryStore{chunks: map[string]store.Chunk{}}
	for _, c := range chunks {
		m.chunks[c.ID] = c
	}
	return m
}

func (m *memoryStore) UpsertChunks(_ context.Context, chunks []store.Chunk, vectors [][]float32) error {
	for i, c := range chunks {
		if i < len(vectors) && len(vectors[i]) > 0 {
			c.Embedding = vectors[i]
		}
		m.chunks[c.ID] = c
	}
	return nil
}

func (m *memoryStore) DeleteChunks(_ context.Context, ids []string) error {
	for _, id := range ids {
		delete(m.chunks, id)
	}
	return nil
}

func (m *memoryStore) DeleteSourceChunks(_ context.Context, sourceID string) error {
	for id, c := range m.chunks {
		if c.SourceID == sourceID {
			delete(m.chunks, id)
		}
	}
	return nil
}

func (m *memoryStore) GetChunk(_ context.Context, id string) (store.Chunk, error) {
	c, ok := m.chunks[id]
	if !ok {
		return store.Chunk{}, store.ErrChunkNotFound
	}
	return c, nil
}

func (m *memoryStore) ListChunks(_ context.Context, filter store.ListFilter) ([]store.Chunk, error) {
	var out []store.Chunk
	for _, c := range m.chunks {
		if filter.SourceID != "" && c.SourceID != filter.SourceID {
			continue
		}
		if filter.LibraryID != "" && c.LibraryID != filter.LibraryID {
			continue
		}
		if filter.Version != "" && c.Version != filter.Version {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (m *memoryStore) CountChunks(_ context.Context, libraryID string) (int, error) {
	n := 0
	for _, c := range m.chunks {
		if libraryID == "" || c.LibraryID == libraryID {
			n++
		}
	}
	return n, nil
}

func (m *memoryStore) Query(ctx context.Context, q store.Query) ([]store.Result, error) {
	chunks, err := m.ListChunks(ctx,
		store.ListFilter{LibraryID: q.LibraryID, Version: q.Version})
	if err != nil {
		return nil, err
	}

	results := rankChunks(chunks, q.Embedding, q.TopK)

	return append([]store.Result(nil), results...), nil
}

func (m *memoryStore) Identity(context.Context) (string, error)  { return "", nil }
func (m *memoryStore) SetIdentity(context.Context, string) error { return nil }
func (m *memoryStore) Close() error                              { return nil }

func rankChunks(chunks []store.Chunk, embedding []float32, topK int) []store.Result {
	if len(embedding) == 0 {
		return nil
	}

	results := make([]store.Result, 0, len(chunks))
	for _, c := range chunks {
		results = append(results, store.Result{Chunk: c, Score: distance(embedding, c.Embedding)})
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].Score < results[j].Score })
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

func distance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1
	}

	var dot, normA, normB float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 1
	}
	return 1 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}
