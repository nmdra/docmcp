package ingest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/source"
	"github.com/docmcp/docmcp/internal/store"
)

// fakeStore records what it was asked to write. It is deliberately transparent:
// every call is visible so a test can assert on what ingestion decided, not just
// on the end state.
type fakeStore struct {
	upserted []store.Chunk
	embedded [][]float32
	deleted  []string

	existing    map[string]store.Chunk
	identity    string
	upsertErr   error
	deleteErr   error
	identityErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{existing: map[string]store.Chunk{}}
}

func (f *fakeStore) UpsertChunks(_ context.Context, chunks []store.Chunk, vectors [][]float32) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = append(f.upserted, chunks...)
	f.embedded = append(f.embedded, vectors...)
	for _, c := range chunks {
		f.existing[c.ID] = c
	}
	return nil
}

func (f *fakeStore) DeleteChunks(_ context.Context, ids []string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, ids...)
	for _, id := range ids {
		delete(f.existing, id)
	}
	return nil
}

func (f *fakeStore) DeleteSourceChunks(_ context.Context, sourceID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	for id, c := range f.existing {
		if c.SourceID == sourceID {
			f.deleted = append(f.deleted, id)
			delete(f.existing, id)
		}
	}
	return nil
}

func (f *fakeStore) GetChunk(_ context.Context, id string) (store.Chunk, error) {
	c, ok := f.existing[id]
	if !ok {
		return store.Chunk{}, store.ErrChunkNotFound
	}
	return c, nil
}

func (f *fakeStore) ListChunks(_ context.Context, filter store.ListFilter) ([]store.Chunk, error) {
	var out []store.Chunk
	for _, c := range f.existing {
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

func (f *fakeStore) CountChunks(_ context.Context, libraryID string) (int, error) {
	n := 0
	for _, c := range f.existing {
		if libraryID == "" || c.LibraryID == libraryID {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) Query(_ context.Context, q store.Query) ([]store.Result, error) {
	return nil, nil
}

func (f *fakeStore) SetIdentity(_ context.Context, identity string) error {
	if f.identityErr != nil {
		return f.identityErr
	}
	f.identity = identity
	return nil
}

func (f *fakeStore) Identity(_ context.Context) (string, error) {
	return f.identity, nil
}

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) chunkIDs() []string {
	out := make([]string, 0, len(f.upserted))
	for _, c := range f.upserted {
		out = append(out, c.ID)
	}
	return out
}

// fakeEmbedder counts calls, which is how the incremental-sync tests prove no
// unnecessary work was done.
type fakeEmbedder struct {
	calls [][]string
	dim   int
	err   error
}

func newFakeEmbedder() *fakeEmbedder {
	return &fakeEmbedder{dim: 4}
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls = append(f.calls, append([]string(nil), texts...))

	if f.err != nil {
		return nil, f.err
	}

	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, f.dim)
		v[0] = float32(len(t))
		v[1] = 1
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) Provider() string { return "fake" }
func (f *fakeEmbedder) Model() string    { return "fake-model" }
func (f *fakeEmbedder) Dimensions() int  { return f.dim }

// embeddedCount is how many texts the embedder was asked to embed in total.
func (f *fakeEmbedder) embeddedCount() int {
	n := 0
	for _, call := range f.calls {
		n += len(call)
	}
	return n
}

// fakeCrawler returns a fixed set of pages, so ingestion is tested without HTTP.
type fakeCrawler struct {
	pages []ingest.Page
	err   error
}

func (f *fakeCrawler) Crawl(context.Context, string) ([]ingest.Page, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.pages, nil
}

// fakeParser turns a page into markdown; the real parser is tested on its own.
type fakeParser struct {
	skip map[string]bool
}

func (f *fakeParser) Parse(page ingest.Page) (ingest.Document, error) {
	if f.skip[page.URL] {
		return ingest.Document{}, ingest.ErrEmptyDocument
	}
	return ingest.Document{
		ID:           page.URL,
		URL:          page.URL,
		CanonicalURL: page.URL,
		Title:        "doc",
		Markdown:     page.Body,
		ContentHash:  page.Body,
	}, nil
}

func newIngestor(t *testing.T, pages []ingest.Page, st *fakeStore, emb *fakeEmbedder) *ingest.Ingestor {
	t.Helper()

	svc, err := ingest.New(
		&fakeCrawler{pages: pages},
		&fakeParser{},
		newChunker(),
		newTestEmbeddings(emb),
		chunkSink{st},
	)
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	return svc
}

func newTestEmbeddings(emb *fakeEmbedder) ingest.Embedder { return emb }

// chunkSink adapts the store-shaped fake to the write surface ingestion needs.
// The types differ on purpose: ingest owns its vocabulary so it does not depend
// on the storage package.
type chunkSink struct {
	*fakeStore
}

func (c chunkSink) ListChunks(ctx context.Context, filter ingest.ListFilter) ([]ingest.Chunk, error) {
	chunks, err := c.fakeStore.ListChunks(ctx, store.ListFilter{
		SourceID:  filter.SourceID,
		LibraryID: filter.LibraryID,
		Version:   filter.Version,
	})
	if err != nil {
		return nil, err
	}

	out := make([]ingest.Chunk, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, toIngestChunk(ch))
	}
	return out, nil
}

func (c chunkSink) UpsertChunks(ctx context.Context, chunks []ingest.Chunk, vectors [][]float32) error {
	converted := make([]store.Chunk, 0, len(chunks))
	for _, ch := range chunks {
		converted = append(converted, fromIngestChunk(ch))
	}
	return c.fakeStore.UpsertChunks(ctx, converted, vectors)
}

func (c chunkSink) DeleteChunks(ctx context.Context, ids []string) error {
	return c.fakeStore.DeleteChunks(ctx, ids)
}

func toIngestChunk(c store.Chunk) ingest.Chunk {
	return ingest.Chunk{
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
	}
}

func fromIngestChunk(c ingest.Chunk) store.Chunk {
	return store.Chunk{
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
	}
}

func testSource() source.Source {
	return source.Source{
		ID:        "src-1",
		Name:      "acme",
		LibraryID: "/local/acme/1",
		Version:   "1",
		BaseURL:   "https://docs.acme.test/",
	}
}

func page(url, body string) ingest.Page {
	return ingest.Page{URL: url, Body: body}
}

const docA = "# API\n\n## Authentication\n\nUse OAuth tokens.\n"
const docB = "# API\n\n## Requests\n\nSend JSON bodies.\n"

var errBoom = errors.New("boom")
