package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	// CandidateCount bounds each dense and lexical list before fusion.
	CandidateCount int

	// FinalChunks is how many chunks a single query returns.
	FinalChunks int
}

func DefaultOptions() Options {
	return Options{CandidateCount: 20, FinalChunks: 6}
}

// Request is one retrieval. QueryEmbedding is optional: when absent the engine
// embeds Query. Explicit technical identifiers in Query enable local lexical
// retrieval; ordinary text and embedding-only requests retain dense ranking.
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
// library and version. Queries with explicit technical identifiers fuse dense
// and local lexical candidates before deduplication and diversification.
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

	ranked := toResults(results)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Chunk.ID < ranked[j].Chunk.ID
		}
		return ranked[i].Score < ranked[j].Score
	})
	if query := strings.TrimSpace(req.Query); hasTechnicalIdentifier(query) {
		chunks, err := e.store.ListChunks(ctx, store.ListFilter{
			LibraryID: libraryID,
			Version:   parsed.Version,
		})
		if err != nil {
			return nil, fmt.Errorf("search: lexical candidates %s: %w", libraryID, err)
		}
		lexical, err := lexicalCandidates(ctx, chunks, query, e.options.CandidateCount)
		if err != nil {
			return nil, fmt.Errorf("search: lexical candidates %s: %w", libraryID, err)
		}
		ranked = RRF(ranked, lexical)
	}
	trimmed := e.trim(ranked)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return trimmed, nil
}

// trim removes empty and duplicate content, then applies a soft cap to adjacent
// chunks from the same section before trimming to the final budget. Exact content
// duplicates are identified by normalized text.
const maxChunksPerAdjacentRun = 2

type adjacentGroup struct {
	sourceID    string
	documentID  string
	headingPath string
}

type adjacentRun struct {
	group      adjacentGroup
	firstIndex int
}

type indexedResult struct {
	resultIndex int
	chunkIndex  int
}

func (e *Engine) trim(results []Result) []Result {
	out := make([]Result, 0, len(results))
	deferred := make([]Result, 0, len(results))
	seen := make(map[string]bool, len(results))
	keptPerRun := make(map[adjacentRun]int, len(results))
	runs := adjacentRuns(results)

	for i, r := range results {
		key := normalizeContent(r.Chunk.Content)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		if run, ok := runs[i]; ok && keptPerRun[run] >= maxChunksPerAdjacentRun {
			deferred = append(deferred, r)
			continue
		}
		if run, ok := runs[i]; ok {
			keptPerRun[run]++
		}

		out = append(out, r)
		if len(out) >= e.options.FinalChunks {
			return out
		}
	}

	// Fill any unused slots only after distinct sections and documents had a
	// chance to contribute. Content and metadata remain untouched.
	for _, r := range deferred {
		if len(out) >= e.options.FinalChunks {
			break
		}
		out = append(out, r)
	}
	return out
}

// adjacentRuns identifies sequential chunk-index runs within one source,
// document, and full heading path. Missing metadata and negative indexes bypass
// structural suppression rather than accidentally grouping unrelated chunks.
func adjacentRuns(results []Result) map[int]adjacentRun {
	groups := make(map[adjacentGroup][]indexedResult)
	for i, result := range results {
		chunk := result.Chunk
		if chunk.SourceID == "" || chunk.DocumentID == "" || chunk.HeadingPath == "" || chunk.Index < 0 {
			continue
		}
		group := adjacentGroup{
			sourceID: chunk.SourceID, documentID: chunk.DocumentID,
			headingPath: chunk.HeadingPath,
		}
		groups[group] = append(groups[group], indexedResult{resultIndex: i, chunkIndex: chunk.Index})
	}

	runs := make(map[int]adjacentRun, len(results))
	for group, indexes := range groups {
		sort.Slice(indexes, func(i, j int) bool {
			if indexes[i].chunkIndex == indexes[j].chunkIndex {
				return indexes[i].resultIndex < indexes[j].resultIndex
			}
			return indexes[i].chunkIndex < indexes[j].chunkIndex
		})

		ambiguous := false
		for i := 1; i < len(indexes); i++ {
			if indexes[i].chunkIndex == indexes[i-1].chunkIndex {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			// Duplicate positions are not reliable evidence of adjacency.
			continue
		}

		firstIndex := 0
		previousIndex := 0
		for i, indexed := range indexes {
			if i == 0 || indexed.chunkIndex > previousIndex+1 {
				firstIndex = indexed.chunkIndex
			}
			runs[indexed.resultIndex] = adjacentRun{group: group, firstIndex: firstIndex}
			previousIndex = indexed.chunkIndex
		}
	}
	return runs
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
