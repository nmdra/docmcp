package app

import (
	"context"

	"github.com/docmcp/docmcp/internal/chunker"
	"github.com/docmcp/docmcp/internal/crawler"
	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/parser"
	"github.com/docmcp/docmcp/internal/store"
)

// crawlerAdapter presents crawler.Crawler as an ingest.Crawler, converting the
// fetch layer's Page into the parser's Page.
//
// The conversion lives here, in the wiring layer, so neither ingest nor crawler
// has to know about the other's types. That is what keeps the dependency
// direction one-way: crawler knows nothing about ingestion.
type crawlerAdapter struct {
	crawler *crawler.Crawler
}

func (a crawlerAdapter) Crawl(ctx context.Context, baseURL string) ([]ingest.Page, error) {
	pages, err := a.crawler.Crawl(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	out := make([]ingest.Page, 0, len(pages))
	for _, p := range pages {
		out = append(out, ingest.Page{URL: p.URL, Body: p.Body})
	}
	return out, nil
}

// parserAdapter presents the HTML parser as an ingest.Parser. The parser speaks
// HTML; ingestion works in Markdown, so this is where that boundary sits.
type parserAdapter struct{}

func (parserAdapter) Parse(page ingest.Page) (ingest.Document, error) {
	doc, err := parser.ParseHTML(parser.Page{URL: page.URL, HTML: page.Body})
	if err != nil {
		return ingest.Document{}, err
	}

	return ingest.Document{
		ID:           doc.ID,
		URL:          doc.URL,
		CanonicalURL: doc.CanonicalURL,
		Title:        doc.Title,
		Markdown:     doc.Markdown,
		ContentHash:  doc.ContentHash,
	}, nil
}

// chunkerAdapter presents the chunker as an ingest.Chunker.
type chunkerAdapter struct {
	chunker chunker.Chunker
}

func (a chunkerAdapter) Chunk(doc ingest.Document, loc ingest.Locator) ([]ingest.Chunk, error) {
	chunks, err := a.chunker.Chunk(
		chunker.Document{
			ID:           doc.ID,
			URL:          doc.URL,
			CanonicalURL: doc.CanonicalURL,
			Title:        doc.Title,
			Markdown:     doc.Markdown,
			ContentHash:  doc.ContentHash,
		},
		chunker.Locator{
			SourceID:  loc.SourceID,
			LibraryID: loc.LibraryID,
			Version:   loc.Version,
		},
	)
	if err != nil {
		return nil, err
	}

	out := make([]ingest.Chunk, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, ingest.Chunk{
			ID:          c.ID,
			SourceID:    c.SourceID,
			LibraryID:   c.LibraryID,
			Version:     c.Version,
			DocumentID:  c.DocumentID,
			URL:         c.URL,
			Title:       c.Title,
			HeadingPath: c.HeadingPath,
			Index:       c.Index,
			Content:     c.Content,
			ContentHash: c.ContentHash,
		})
	}
	return out, nil
}

// chunkSink presents the store as the write surface ingestion needs. Ingestion
// depends on writes, not on the full Store contract.
type chunkSink struct {
	store store.Store
}

func (s chunkSink) UpsertChunks(ctx context.Context, chunks []ingest.Chunk, vectors [][]float32) error {
	converted := make([]store.Chunk, 0, len(chunks))
	for _, c := range chunks {
		converted = append(converted, store.Chunk{
			ID:          c.ID,
			SourceID:    c.SourceID,
			LibraryID:   c.LibraryID,
			Version:     c.Version,
			DocumentID:  c.DocumentID,
			URL:         c.URL,
			Title:       c.Title,
			HeadingPath: c.HeadingPath,
			Index:       c.Index,
			Content:     c.Content,
			ContentHash: c.ContentHash,
		})
	}
	return s.store.UpsertChunks(ctx, converted, vectors)
}

func (s chunkSink) DeleteChunks(ctx context.Context, ids []string) error {
	return s.store.DeleteChunks(ctx, ids)
}

func (s chunkSink) ListChunks(ctx context.Context, filter ingest.ListFilter) ([]ingest.Chunk, error) {
	chunks, err := s.store.ListChunks(ctx, store.ListFilter{
		SourceID:  filter.SourceID,
		LibraryID: filter.LibraryID,
		Version:   filter.Version,
	})
	if err != nil {
		return nil, err
	}

	out := make([]ingest.Chunk, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, ingest.Chunk{
			ID:          c.ID,
			SourceID:    c.SourceID,
			LibraryID:   c.LibraryID,
			Version:     c.Version,
			DocumentID:  c.DocumentID,
			URL:         c.URL,
			Title:       c.Title,
			HeadingPath: c.HeadingPath,
			Index:       c.Index,
			Content:     c.Content,
			ContentHash: c.ContentHash,
		})
	}
	return out, nil
}

func (s chunkSink) Identity(ctx context.Context) (string, error) {
	return s.store.Identity(ctx)
}

func (s chunkSink) SetIdentity(ctx context.Context, identity string) error {
	return s.store.SetIdentity(ctx, identity)
}

// newIngestor assembles the production ingestion pipeline. Every adapter above
// exists so this function is the only place the concrete types meet.
func newIngestor(c *crawler.Crawler, ch chunker.Chunker, st store.Store, emb ingest.Embedder) (*ingest.Ingestor, error) {
	return ingest.New(
		crawlerAdapter{crawler: c},
		parserAdapter{},
		chunkerAdapter{chunker: ch},
		emb,
		chunkSink{store: st},
	)
}
