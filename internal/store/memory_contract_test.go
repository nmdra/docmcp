package store_test

import (
	"context"
	"math"
	"sort"
	"sync"
	"testing"

	"github.com/nmdra/docmcp/internal/store"
)

// TestMemoryStoreContract runs the shared suite against an in-memory store. It
// is the default-suite guard: if the contract drifts, this fails without needing
// Chroma or the integration build tag.
func TestMemoryStoreContract(t *testing.T) {
	RunStoreContractTests(t, newMemoryStore)
}

// memoryStore is a minimal in-process Store. It is not shipped — it exists so the
// contract has a second, obviously-correct implementation to check against.
type memoryStore struct {
	mu       sync.Mutex
	chunks   map[string]storedChunk
	identity string
}

type storedChunk struct {
	store.Chunk
	embedding []float32
}

func newMemoryStore(t *testing.T) store.Store {
	t.Helper()
	return &memoryStore{chunks: map[string]storedChunk{}}
}

func (m *memoryStore) UpsertChunks(_ context.Context, chunks []store.Chunk, embeddings [][]float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, c := range chunks {
		if c.ID == "" {
			return store.ErrInvalidChunk
		}
		if i < len(embeddings) {
			c.Embedding = embeddings[i]
		}
		m.chunks[c.ID] = storedChunk{Chunk: c, embedding: c.Embedding}
	}
	return nil
}

func (m *memoryStore) DeleteChunks(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		delete(m.chunks, id)
	}
	return nil
}

func (m *memoryStore) DeleteSourceChunks(_ context.Context, sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, c := range m.chunks {
		if c.SourceID == sourceID {
			delete(m.chunks, id)
		}
	}
	return nil
}

func (m *memoryStore) GetChunk(_ context.Context, id string) (store.Chunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.chunks[id]
	if !ok {
		return store.Chunk{}, store.ErrChunkNotFound
	}
	return c.Chunk, nil
}

func (m *memoryStore) ListChunks(_ context.Context, filter store.ListFilter) ([]store.Chunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []store.Chunk
	for _, c := range m.chunks {
		if matches(c.Chunk, filter) {
			out = append(out, c.Chunk)
		}
	}
	sortChunksByID(out)
	return out, nil
}

func (m *memoryStore) CountChunks(_ context.Context, libraryID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0
	for _, c := range m.chunks {
		if libraryID == "" || c.LibraryID == libraryID {
			n++
		}
	}
	return n, nil
}

func (m *memoryStore) Query(_ context.Context, q store.Query) ([]store.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	filter := store.ListFilter{LibraryID: q.LibraryID, Version: q.Version}
	var candidates []store.Chunk
	for _, c := range m.chunks {
		if matches(c.Chunk, filter) {
			candidates = append(candidates, c.Chunk)
		}
	}
	sortChunksByID(candidates)

	return rank(candidates, q.Embedding, q.TopK), nil
}

func (m *memoryStore) SetIdentity(_ context.Context, identity string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.identity = identity
	return nil
}

func (m *memoryStore) Identity(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.identity, nil
}

func (m *memoryStore) Close() error { return nil }

func matches(c store.Chunk, f store.ListFilter) bool {
	if f.SourceID != "" && c.SourceID != f.SourceID {
		return false
	}
	if f.LibraryID != "" && c.LibraryID != f.LibraryID {
		return false
	}
	if f.Version != "" && c.Version != f.Version {
		return false
	}
	return true
}

func sortChunksByID(chunks []store.Chunk) {
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })
}

func rank(chunks []store.Chunk, embedding []float32, topK int) []store.Result {
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
