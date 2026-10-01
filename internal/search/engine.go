package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docmcp/docmcp/internal/source"
	"github.com/docmcp/docmcp/internal/store"
)

var (
	ErrEmptyQuery     = errors.New("a query is required")
	ErrUnknownLibrary = errors.New("library is not indexed")
)

// Options bound one retrieval. Both are internal: they never reach the MCP
// surface, where the contract is two required fields and nothing else.
type Options struct {
	// CandidateCount is how many vector hits are pulled before deduplication.
	CandidateCount int

	// FinalChunks is how many chunks a single query returns.
	FinalChunks int
}

func DefaultOptions() Options {
	return Options{CandidateCount: 20, FinalChunks: 6}
}

// Request is one retrieval. QueryEmbedding is optional: when absent the engine
// embeds Query, which is what both callers want.
type Request struct {
	LibraryID string
	Query     string

	// QueryEmbedding bypasses the embedder. It exists so retrieval tests can
	// assert ordering against a known vector space.
	QueryEmbedding []float32
}

// Result is one retrieved chunk plus the internal score used for ordering.
type Result struct {
	Chunk store.Chunk
	Score float64
}

// Format renders a result as neutral, citable text.
//
// It deliberately carries no distance, score, vector ID, or topK. Those are
// implementation details: an agent shown a distance learns nothing useful and
// tends to over-trust the number.
func (r Result) Format() string {
	heading := r.Chunk.HeadingPath
	if heading == "" {
		heading = r.Chunk.Title
	}
	if heading == "" {
		heading = r.Chunk.URL
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", heading)
	fmt.Fprintf(&b, "Source: %s\n", r.Chunk.URL)
	fmt.Fprintf(&b, "Library: %s\n\n", r.Chunk.LibraryID)
	b.WriteString(strings.TrimSpace(r.Chunk.Content))
	b.WriteString("\n")

	return b.String()
}

// FormatAll renders results separated by horizontal rules.
func FormatAll(results []Result) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		parts = append(parts, r.Format())
	}
	return strings.Join(parts, "\n---\n\n")
}

// Embedder turns a query into a vector.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Engine retrieves documentation for one library.
type Engine struct {
	store    store.Store
	embedder Embedder
	options  Options
}

func NewEngine(st store.Store, embedder Embedder, opts Options) (*Engine, error) {
	if st == nil {
		return nil, fmt.Errorf("search: a store is required")
	}
	if embedder == nil {
		return nil, fmt.Errorf("search: an embedder is required")
	}

	if opts.CandidateCount <= 0 {
		opts.CandidateCount = 20
	}
	if opts.FinalChunks <= 0 {
		opts.FinalChunks = 6
	}

	return &Engine{store: st, embedder: embedder, options: opts}, nil
}

// Options returns the internal retrieval budget.
func (e *Engine) Options() Options { return e.options }

// Search retrieves the chunks most relevant to a query, restricted to one
// library.
func (e *Engine) Search(ctx context.Context, req Request) ([]Result, error) {
	libraryID := strings.TrimSpace(req.LibraryID)
	if libraryID == "" {
		return nil, fmt.Errorf("search: %w: a library ID is required", ErrEmptyQuery)
	}

	parsed, err := source.ParseLibraryID(libraryID)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	embedding := req.QueryEmbedding
	if len(embedding) == 0 {
		query := strings.TrimSpace(req.Query)
		if query == "" {
			return nil, fmt.Errorf("search: %w", ErrEmptyQuery)
		}

		vectors, err := e.embedder.Embed(ctx, []string{query})
		if err != nil {
			return nil, fmt.Errorf("search: embed query: %w", err)
		}
		if len(vectors) != 1 {
			return nil, fmt.Errorf("search: embedder returned %d vectors for one query", len(vectors))
		}
		embedding = vectors[0]
	}

	results, err := e.store.Query(ctx, store.Query{
		LibraryID: libraryID,
		Version:   parsed.Version,
		Embedding: embedding,
		TopK:      e.options.CandidateCount,
	})
	if err != nil {
		return nil, fmt.Errorf("search: query %s: %w", libraryID, err)
	}

	return e.trim(toResults(results)), nil
}

// trim deduplicates near-identical chunks and trims to the final budget.
// Deduplication is by normalized content: a section split across several chunks
// would otherwise fill the whole answer with near-copies.
func (e *Engine) trim(results []Result) []Result {
	out := make([]Result, 0, len(results))
	seen := make(map[string]bool, len(results))

	for _, r := range results {
		key := normalizeContent(r.Chunk.Content)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		out = append(out, r)
		if len(out) >= e.options.FinalChunks {
			break
		}
	}

	return out
}

// toResults adapts the store's results to this package's, dropping nothing:
// Score stays internal here and is never formatted into an answer.
func toResults(in []store.Result) []Result {
	out := make([]Result, 0, len(in))
	for _, r := range in {
		out = append(out, Result{Chunk: r.Chunk, Score: r.Score})
	}
	return out
}

func normalizeContent(s string) string {
	fields := strings.Fields(strings.ToLower(s))
	return strings.Join(fields, " ")
}
