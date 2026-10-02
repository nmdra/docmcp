// Package ingest joins the crawl → parse → chunk → embed → store pipeline into
// the one operation the CLI performs. Every collaborator arrives as an
// interface, so the whole path is testable without HTTP or a vector store.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docmcp/docmcp/internal/embedding"
	"github.com/docmcp/docmcp/internal/source"
)

var (
	// ErrEmptyDocument reports a page with no indexable content. It is a skip,
	// not a failure: one blank page should not fail a whole site.
	ErrEmptyDocument = errors.New("empty document")
)

// Page is one fetched document, before parsing. The title is not carried here:
// the parser derives it from the HTML, and a second, weaker source of truth for
// it would eventually disagree.
type Page struct {
	URL  string
	Body string
}

// Document is a parsed page, ready to chunk.
type Document struct {
	ID           string
	URL          string
	CanonicalURL string
	Title        string
	Markdown     string
	ContentHash  string
}

// embeddingTextVersion changes when the text representation sent to the embedder
// changes. Indexes with another representation must be reindexed before sync.
const embeddingTextVersion = "title-heading-v1"

// Chunk is one retrievable section of a document.
type Chunk struct {
	ID        string
	SourceID  string
	LibraryID string
	Version   string

	DocumentID  string
	URL         string
	Title       string
	HeadingPath string

	Index       int
	Content     string
	ContentHash string
}

// EmbeddingText adds document context to a chunk for vector generation only.
// The returned text is not stored or exposed in search results.
func EmbeddingText(chunk Chunk) string {
	if strings.EqualFold(strings.TrimSpace(chunk.Title), strings.TrimSpace(chunk.HeadingPath)) {
		chunk.HeadingPath = ""
	}

	parts := make([]string, 0, 3)
	for _, part := range []string{chunk.Title, chunk.HeadingPath, chunk.Content} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Locator binds chunks to the library being indexed.
type Locator struct {
	SourceID  string
	LibraryID string
	Version   string
}

// Crawler discovers and fetches a source's pages.
type Crawler interface {
	Crawl(ctx context.Context, baseURL string) ([]Page, error)
}

// Parser turns a page into a document.
type Parser interface {
	Parse(page Page) (Document, error)
}

// Chunker splits a document into retrievable chunks.
type Chunker interface {
	Chunk(doc Document, loc Locator) ([]Chunk, error)
}

// Embedder turns chunk text into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Provider() string
	Model() string
	Dimensions() int
}

// Chunks is the store's write surface. Ingestion depends on the writes it needs,
// not the whole Store: the service that owns storage decides what reads look like.
type Chunks interface {
	UpsertChunks(ctx context.Context, chunks []Chunk, vectors [][]float32) error
	DeleteChunks(ctx context.Context, ids []string) error
	ListChunks(ctx context.Context, filter ListFilter) ([]Chunk, error)
	Identity(ctx context.Context) (string, error)
	SetIdentity(ctx context.Context, identity string) error
}

// ListFilter narrows a listing by source or library.
type ListFilter struct {
	SourceID  string
	LibraryID string
	Version   string
}

// Report is what one ingest run did, for CLI output and tests.
type Report struct {
	PagesDiscovered int
	PagesFetched    int
	PagesSkipped    int

	ChunksUnchanged int
	ChunksUpdated   int
	ChunksAdded     int
	ChunksRemoved   int

	// Sample is a small set of the chunks this run wrote, for tests and
	// diagnostics. It is not a stable API.
	Sample []Chunk
}

// Ingestor runs the pipeline for one source at a time.
type Ingestor struct {
	crawler  Crawler
	parser   Parser
	chunker  Chunker
	embedder Embedder
	chunks   Chunks
}

func New(crawler Crawler, parser Parser, chunker Chunker, embedder Embedder, chunks Chunks) (*Ingestor, error) {
	switch {
	case crawler == nil:
		return nil, fmt.Errorf("ingest: crawler is required")
	case parser == nil:
		return nil, fmt.Errorf("ingest: parser is required")
	case chunker == nil:
		return nil, fmt.Errorf("ingest: chunker is required")
	case embedder == nil:
		return nil, fmt.Errorf("ingest: embedder is required")
	case chunks == nil:
		return nil, fmt.Errorf("ingest: chunk store is required")
	}

	return &Ingestor{
		crawler:  crawler,
		parser:   parser,
		chunker:  chunker,
		embedder: embedder,
		chunks:   chunks,
	}, nil
}

// Ingest crawls a source, chunks what changed, embeds only what needs it, and
// removes chunks whose pages are gone.
//
// The incremental rule: a chunk ID identifies logical position and its content
// hash identifies content. Same ID plus same hash is a skip; same ID plus a
// different hash is a re-embed.
func (i *Ingestor) Ingest(ctx context.Context, src source.Source) (Report, error) {
	var report Report

	identity, err := i.chunks.Identity(ctx)
	if err != nil {
		return report, fmt.Errorf("ingest: read index identity: %w", err)
	}
	if err := i.checkIdentity(identity); err != nil {
		return report, err
	}

	pages, err := i.crawler.Crawl(ctx, src.BaseURL)
	if err != nil {
		return report, fmt.Errorf("ingest: crawl %s: %w", src.LibraryID, err)
	}
	report.PagesDiscovered = len(pages)

	locator := Locator{
		SourceID:  src.ID,
		LibraryID: src.LibraryID,
		Version:   src.Version,
	}

	existing, err := i.chunks.ListChunks(ctx, ListFilter{SourceID: src.ID})
	if err != nil {
		return report, fmt.Errorf("ingest: list existing chunks: %w", err)
	}
	existingByID := make(map[string]Chunk, len(existing))
	for _, c := range existing {
		existingByID[c.ID] = c
	}

	var (
		seen    = map[string]bool{}
		toEmbed []Chunk
		toWrite []Chunk
		removed []string
	)

	for _, p := range pages {
		doc, err := i.parser.Parse(p)
		if err != nil {
			if errors.Is(err, ErrEmptyDocument) {
				report.PagesSkipped++
				continue
			}
			return report, fmt.Errorf("ingest: parse %s: %w", p.URL, err)
		}
		report.PagesFetched++

		chunks, err := i.chunker.Chunk(doc, locator)
		if err != nil {
			return report, fmt.Errorf("ingest: chunk %s: %w", p.URL, err)
		}

		for _, c := range chunks {
			seen[c.ID] = true

			previous, found := existingByID[c.ID]
			if found && previous.ContentHash == c.ContentHash &&
				previous.Title == c.Title && previous.HeadingPath == c.HeadingPath {
				report.ChunksUnchanged++
				continue
			}

			if found {
				report.ChunksUpdated++
			} else {
				report.ChunksAdded++
			}
			toEmbed = append(toEmbed, c)
			toWrite = append(toWrite, c)
		}
	}

	// Anything previously stored for this source but no longer produced is gone.
	for _, c := range existing {
		if !seen[c.ID] {
			removed = append(removed, c.ID)
		}
	}
	report.ChunksRemoved = len(removed)

	dimensions := i.embedder.Dimensions()
	if identity != "" && dimensions == 0 {
		dimensions, _ = identityDimensions(identity, i.embedder)
	}
	vectors, err := i.embedAll(ctx, toEmbed)
	if err != nil {
		return report, err
	}

	// Validate the complete batch against the persisted width before any mutation.
	for _, vector := range vectors {
		if err := embedding.ValidateEmbedding(vector, dimensions); err != nil {
			return report, fmt.Errorf("ingest: index embedding width is incompatible: %w; run docmcp reindex (use a fresh --data-dir for a shared index)", err)
		}
		if dimensions == 0 {
			dimensions = len(vector)
		}
	}

	if len(toWrite) > 0 {
		if err := i.chunks.UpsertChunks(ctx, toWrite, vectors); err != nil {
			return report, fmt.Errorf("ingest: write %d chunks: %w", len(toWrite), err)
		}
		report.Sample = toWrite
	}

	if len(removed) > 0 {
		if err := i.chunks.DeleteChunks(ctx, removed); err != nil {
			return report, fmt.Errorf("ingest: delete %d stale chunks: %w", len(removed), err)
		}
	}

	// An empty unknown-width run cannot establish an embedding fingerprint.
	if identity == "" && dimensions > 0 {
		identity = embeddingIdentity(i.embedder, dimensions)
		if err := i.chunks.SetIdentity(ctx, identity); err != nil {
			return report, fmt.Errorf("ingest: record index identity: %w", err)
		}
	}

	return report, nil
}

// Remove drops every chunk belonging to a source. It is separate from Ingest
// because a removed source has nothing to crawl.
func (i *Ingestor) Remove(ctx context.Context, sourceID string) error {
	existing, err := i.chunks.ListChunks(ctx, ListFilter{SourceID: sourceID})
	if err != nil {
		return fmt.Errorf("ingest: list chunks for source %q: %w", sourceID, err)
	}
	if len(existing) == 0 {
		return nil
	}

	ids := make([]string, 0, len(existing))
	for _, c := range existing {
		ids = append(ids, c.ID)
	}

	if err := i.chunks.DeleteChunks(ctx, ids); err != nil {
		return fmt.Errorf("ingest: remove %d chunks for source %q: %w", len(ids), sourceID, err)
	}
	return nil
}

// embedAll embeds only the chunks that need it, then releases the caller of any
// vector when the embedder is misbehaving.
func (i *Ingestor) embedAll(ctx context.Context, chunks []Chunk) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	texts := make([]string, len(chunks))
	for n, c := range chunks {
		texts[n] = EmbeddingText(c)
	}

	vectors, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("ingest: embed %d chunks: %w", len(chunks), err)
	}
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("ingest: embedder returned %d vectors for %d chunks",
			len(vectors), len(chunks))
	}
	return vectors, nil
}

// identity is the fingerprint persisted with the index. One embedding model per
// index: a change here must force a reindex rather than mix vector spaces.
func (i *Ingestor) identity() string {
	return EmbeddingIdentity(i.embedder)
}

// EmbeddingIdentity identifies both the model and its chunk-text representation.
// Application services use it before clearing an index identity for reindexing.
func EmbeddingIdentity(embedder Embedder) string {
	return embeddingIdentity(embedder, embedder.Dimensions())
}

// checkIdentity refuses to write into an index built by a different model.
func (i *Ingestor) checkIdentity(existing string) error {
	if existing == "" || EmbeddingIdentityCompatible(existing, i.embedder) {
		return nil
	}

	return fmt.Errorf(
		"embedding configuration changed.\n\n"+
			"Existing index:\n  %s\n\n"+
			"Configured:\n  %s\n\n"+
			"Run:\n  docmcp reindex",
		existing, i.identity())
}

func embeddingIdentity(embedder Embedder, dimensions int) string {
	return fmt.Sprintf("%s/%s/%d/%s", embedder.Provider(), embedder.Model(), dimensions, embeddingTextVersion)
}

// EmbeddingIdentityCompatible requires the same provider, model, and text format.
// An unknown configured width accepts only a recorded positive actual width.
func EmbeddingIdentityCompatible(existing string, embedder Embedder) bool {
	if embedder.Dimensions() != 0 {
		return existing == EmbeddingIdentity(embedder)
	}
	dimensions, ok := identityDimensions(existing, embedder)
	return ok && dimensions > 0
}

func identityDimensions(identity string, embedder Embedder) (int, bool) {
	prefix := embedder.Provider() + "/" + embedder.Model() + "/"
	suffix := "/" + embeddingTextVersion
	if !strings.HasPrefix(identity, prefix) || !strings.HasSuffix(identity, suffix) {
		return 0, false
	}
	width := strings.TrimSuffix(strings.TrimPrefix(identity, prefix), suffix)
	dimensions, err := strconv.Atoi(width)
	return dimensions, err == nil && width == strconv.Itoa(dimensions)
}
