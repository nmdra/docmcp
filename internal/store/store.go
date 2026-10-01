package store

import (
	"context"
	"errors"
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

func validateChunk(c Chunk) error {
	if strings.TrimSpace(c.ID) == "" {
		return ErrInvalidChunk
	}
	return nil
}
