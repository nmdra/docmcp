package store

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
)

var (
	// ErrChunkNotFound reports a chunk ID that is not in the index.
	ErrChunkNotFound = errors.New("chunk not found")

	// ErrInvalidChunk reports input the store cannot key or store.
	ErrInvalidChunk = errors.New("invalid chunk")
)

// Chunk is the persisted form of a retrievable documentation section.
type Chunk struct {
	ID        string
	SourceID  string
	LibraryID string
	Version   string

	DocumentID  string
	URL         string
	Title       string
	HeadingPath string

	Index   int
	Content string

	ContentHash string

	// Embedding is the vector for Content. It is carried alongside the chunk so
	// the ingestion service can stage a chunk before the embedder has run.
	Embedding []float32
}

// Result is one scored hit. Score is internal: it never crosses the MCP boundary.
type Result struct {
	Chunk Chunk
	Score float64
}

// ListFilter narrows a listing. An empty field means "any".
type ListFilter struct {
	SourceID  string
	LibraryID string
	Version   string
}

// Query is a semantic search restricted to one library.
type Query struct {
	LibraryID string
	Version   string
	Embedding []float32
	TopK      int
}

// Store persists chunks and answers queries. MCP queries and CLI search both go
// through this one interface, so neither can drift from the other.
type Store interface {
	UpsertChunks(ctx context.Context, chunks []Chunk, embeddings [][]float32) error
	DeleteChunks(ctx context.Context, ids []string) error
	DeleteSourceChunks(ctx context.Context, sourceID string) error

	GetChunk(ctx context.Context, id string) (Chunk, error)
	ListChunks(ctx context.Context, filter ListFilter) ([]Chunk, error)
	CountChunks(ctx context.Context, libraryID string) (int, error)

	Query(ctx context.Context, q Query) ([]Result, error)

	// SetIdentity and Identity record which embedding model wrote this index.
	// A mismatch is refused rather than silently mixing vector spaces.
	SetIdentity(ctx context.Context, identity string) error
	Identity(ctx context.Context) (string, error)

	Close() error
}

// cosineDistance is the ranking metric. Chroma's default distance for normalized
// vectors is equivalent, so results order the same way across implementations.
func cosineDistance(a, b []float32) float64 {
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

// rankChunks orders chunks by distance to the query vector, closest first.
func rankChunks(chunks []Chunk, embedding []float32, topK int) []Result {
	if len(chunks) == 0 || len(embedding) == 0 {
		return nil
	}

	results := make([]Result, 0, len(chunks))
	for _, c := range chunks {
		results = append(results, Result{
			Chunk: c,
			Score: cosineDistance(embedding, c.Embedding),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score < results[j].Score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

// matchesFilter reports whether a chunk satisfies every non-empty filter field.
func matchesFilter(c Chunk, f ListFilter) bool {
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

func validateChunk(c Chunk) error {
	if strings.TrimSpace(c.ID) == "" {
		return ErrInvalidChunk
	}
	return nil
}
