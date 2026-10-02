package ingest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/chunker"
	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/store"
)

// newChunker wires the real chunker, so chunk IDs and hashes under test are the
// ones production produces. Faking the chunker here would test nothing.
func newChunker() ingest.Chunker {
	c, err := chunker.NewMarkdownChunker()
	if err != nil {
		panic(err)
	}
	return chunkerAdapter{c}
}

// chunkerAdapter bridges the chunker's own types to the ingestion interfaces.
type chunkerAdapter struct {
	c chunker.Chunker
}

func (a chunkerAdapter) Chunk(doc ingest.Document, loc ingest.Locator) ([]ingest.Chunk, error) {
	chunks, err := a.c.Chunk(
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

func TestEmbeddingText_IncludesTitleAndHeadingPath(t *testing.T) {
	chunk := ingest.Chunk{
		Title:       "Pi Documentation",
		HeadingPath: "MCP > Control tool exposure",
		Content:     "Tools can be exposed directly.",
	}

	got := ingest.EmbeddingText(chunk)
	want := "Pi Documentation\n\nMCP > Control tool exposure\n\n" + chunk.Content
	if got != want {
		t.Errorf("EmbeddingText() = %q, want %q", got, want)
	}
}

func TestEmbeddingText_DeduplicatesRepeatedTitleAndHeading(t *testing.T) {
	chunk := ingest.Chunk{
		Title:       "Authentication > OAuth",
		HeadingPath: "Authentication > OAuth",
		Content:     "Exchange an authorization code.",
	}

	got := ingest.EmbeddingText(chunk)
	want := "Authentication > OAuth\n\nExchange an authorization code."
	if got != want {
		t.Errorf("EmbeddingText() = %q, want duplicate metadata omitted: %q", got, want)
	}
}

func TestEmbeddingText_DoesNotModifyStoredContent(t *testing.T) {
	chunk := ingest.Chunk{
		Title:       "Pi Documentation",
		HeadingPath: "MCP > Control tool exposure",
		Content:     "Tools can be exposed directly.",
		ContentHash: "stable-hash",
	}

	_ = ingest.EmbeddingText(chunk)
	if chunk.Content != "Tools can be exposed directly." || chunk.ContentHash != "stable-hash" {
		t.Fatalf("embedding text changed stored chunk content: %+v", chunk)
	}
}

func TestEmbeddingText_IsStable(t *testing.T) {
	chunk := ingest.Chunk{
		Title:       "Pi Documentation",
		HeadingPath: "MCP > Control tool exposure",
		Content:     "Tools can be exposed directly.",
	}

	first := ingest.EmbeddingText(chunk)
	for range 3 {
		if got := ingest.EmbeddingText(chunk); got != first {
			t.Fatalf("EmbeddingText changed between calls: %q then %q", first, got)
		}
	}
}

func TestIngestor_AddWebsite(t *testing.T) {
	st := newFakeStore()
	emb := newFakeEmbedder()

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
		page("https://docs.acme.test/requests", docB),
	}, st, emb)

	report, err := ing.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if report.PagesDiscovered != 2 {
		t.Errorf("PagesDiscovered = %d, want 2", report.PagesDiscovered)
	}
	if report.PagesFetched != 2 {
		t.Errorf("PagesFetched = %d, want 2", report.PagesFetched)
	}
	if report.ChunksAdded == 0 {
		t.Error("ChunksAdded = 0, want the site indexed")
	}
	if len(st.upserted) == 0 {
		t.Error("nothing was written to the store")
	}
}

func TestIngestor_StoresChunks(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	for _, c := range st.upserted {
		if c.SourceID != "src-1" {
			t.Errorf("chunk %q has SourceID %q, want src-1", c.ID, c.SourceID)
		}
		if c.LibraryID != "/local/acme/1" {
			t.Errorf("chunk %q has LibraryID %q, want /local/acme/1", c.ID, c.LibraryID)
		}
		if c.Version != "1" {
			t.Errorf("chunk %q has Version %q, want 1", c.ID, c.Version)
		}
		if c.Content == "" {
			t.Errorf("chunk %q has empty content", c.ID)
		}
	}
}

func TestIngestor_StoresMetadata(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var found bool
	for _, c := range st.upserted {
		if c.HeadingPath != "API > Authentication" {
			continue
		}
		found = true
		if c.URL != "https://docs.acme.test/api" {
			t.Errorf("URL = %q, want the page URL so results are citable", c.URL)
		}
		if c.Title != "doc" {
			t.Errorf("Title = %q, want the title the parser produced", c.Title)
		}
		if c.DocumentID == "" {
			t.Error("DocumentID is empty")
		}
	}
	if !found {
		t.Errorf("no chunk carried heading path %q; got %+v", "API > Authentication", st.upserted)
	}
}

func TestIngestor_ReembedsWhenTitleChangesWithoutBodyChanges(t *testing.T) {
	pages := []ingest.Page{page("https://docs.acme.test/api", docA)}
	st := newFakeStore()
	if _, err := newIngestor(t, pages, st, newFakeEmbedder()).Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("initial Ingest: %v", err)
	}

	// Seed a previous title while keeping the IDs, body, and content hash fixed.
	for id, saved := range st.existing {
		saved.Title = "Previous document title"
		st.existing[id] = saved
	}
	st.upserted = nil
	emb := newFakeEmbedder()
	report, err := newIngestor(t, pages, st, emb).Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if emb.embeddedCount() == 0 || report.ChunksUpdated == 0 {
		t.Fatalf("title changed but chunks were not reembedded: %+v", report)
	}
	for _, saved := range st.upserted {
		if saved.Title != "doc" {
			t.Errorf("stored title = %q, want current title", saved.Title)
		}
	}
}

func TestIngestor_SkipsUnchangedChunks(t *testing.T) {
	pages := []ingest.Page{page("https://docs.acme.test/api", docA)}

	st := newFakeStore()
	first := newFakeEmbedder()
	ing := newIngestor(t, pages, st, first)

	report, err := ing.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if report.ChunksAdded == 0 {
		t.Fatal("first ingest added nothing")
	}
	embeddedFirst := first.embeddedCount()
	if embeddedFirst == 0 {
		t.Fatal("first ingest embedded nothing")
	}

	// Second run over identical content: the whole point is zero embeddings.
	st2 := newFakeStore()
	for _, c := range st.upserted {
		st2.existing[c.ID] = c
	}
	second := newFakeEmbedder()
	ing2 := newIngestor(t, pages, st2, second)

	report2, err := ing2.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if second.embeddedCount() != 0 {
		t.Errorf("re-ingesting unchanged content embedded %d chunks, want 0",
			second.embeddedCount())
	}
	if len(st2.upserted) != 0 {
		t.Errorf("re-ingesting unchanged content rewrote %d chunks, want 0", len(st2.upserted))
	}
	if report2.ChunksUnchanged == 0 {
		t.Error("ChunksUnchanged = 0, want the existing chunks recognised as unchanged")
	}
}

func TestIngestor_UpdatesChangedChunks(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{page("https://docs.acme.test/api", docA)}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	originalID := st.upserted[0].ID

	changed := "# API\n\n## Authentication\n\nUse OAuth2 refresh tokens.\n"
	st2 := newFakeStore()
	for _, c := range st.upserted {
		st2.existing[c.ID] = c
	}
	emb := newFakeEmbedder()
	ing2 := newIngestor(t, []ingest.Page{page("https://docs.acme.test/api", changed)}, st2, emb)

	report, err := ing2.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if emb.embeddedCount() == 0 {
		t.Error("a content change embedded nothing, want a re-embed")
	}
	if report.ChunksUpdated == 0 {
		t.Errorf("ChunksUpdated = 0, want the changed chunk reported; report=%+v", report)
	}
	if len(st2.upserted) != 1 {
		t.Fatalf("wrote %d chunks, want only the changed one", len(st2.upserted))
	}
	// The ID identifies logical position, so an edit must not change it.
	if got := st2.upserted[0].ID; got != originalID {
		t.Errorf("chunk ID changed on edit: %q -> %q", originalID, got)
	}
	if st2.upserted[0].ContentHash == st.existing[originalID].ContentHash {
		t.Error("ContentHash unchanged after a content edit")
	}
}

func TestIngestor_RemovesDeletedChunks(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
		page("https://docs.acme.test/requests", docB),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	before := len(st.upserted)
	if before < 2 {
		t.Fatalf("expected chunks from two pages, got %d", before)
	}

	// Only one page remains in the second run.
	kept := map[string]string{}
	for _, c := range st.upserted {
		kept[c.ID] = c.URL
	}
	var survivorID string
	for _, c := range st.upserted {
		if strings.Contains(c.Content, "OAuth") {
			survivorID = c.ID
		}
	}
	if survivorID == "" {
		t.Fatal("could not identify the surviving chunk")
	}

	st2 := newFakeStore()
	for _, c := range st.upserted {
		st2.existing[c.ID] = c
	}
	ing2 := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st2, newFakeEmbedder())

	report, err := ing2.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if report.ChunksRemoved == 0 {
		t.Errorf("ChunksRemoved = 0 after a page disappeared; report=%+v", report)
	}

	for _, id := range st2.deleted {
		if id == survivorID {
			t.Errorf("deleted the surviving chunk %q", id)
		}
	}
	if _, ok := st2.existing[survivorID]; !ok {
		t.Error("the surviving chunk was removed from the store")
	}
}

func TestIngestor_ChunkIDsAreStableAcrossRuns(t *testing.T) {
	pages := []ingest.Page{
		page("https://docs.acme.test/api", docA),
		page("https://docs.acme.test/requests", docB),
	}

	first := newIngestor(t, pages, newFakeStore(), newFakeEmbedder())
	report1, err := first.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	st2 := newFakeStore()
	second := newIngestor(t, pages, st2, newFakeEmbedder())
	if _, err := second.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	ids := map[string]bool{}
	for _, c := range st2.upserted {
		ids[c.ID] = true
	}
	for _, c := range report1.Sample {
		if !ids[c.ID] {
			t.Errorf("chunk ID %q differed between runs", c.ID)
		}
	}
}

func TestIngestor_SkipsEmptyDocuments(t *testing.T) {
	st := newFakeStore()
	emb := newFakeEmbedder()

	pages := []ingest.Page{
		page("https://docs.acme.test/api", docA),
		page("https://docs.acme.test/blank", "   "),
	}

	svc, err := ingest.New(
		&fakeCrawler{pages: pages},
		&fakeParser{skip: map[string]bool{"https://docs.acme.test/blank": true}},
		newChunker(),
		emb,
		chunkSink{st},
	)
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}

	report, err := svc.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	for _, c := range st.upserted {
		if strings.Contains(c.URL, "blank") {
			t.Errorf("indexed an empty page: %+v", c)
		}
	}
	if report.PagesSkipped != 1 {
		t.Errorf("PagesSkipped = %d, want 1", report.PagesSkipped)
	}
}

func TestIngestor_RecordsEmbeddingIdentity(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{page("https://docs.acme.test/api", docA)}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if st.identity == "" {
		t.Fatal("no embedding identity was recorded with the index")
	}
	if !strings.Contains(st.identity, "fake-model") {
		t.Errorf("identity = %q, want it to name the model", st.identity)
	}
}

func TestIngestor_RefusesWhenEmbeddingModelChanged(t *testing.T) {
	st := newFakeStore()
	st.identity = "ollama/nomic-embed-text/768"

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	_, err := ing.Ingest(t.Context(), testSource())
	if err == nil {
		t.Fatal("Ingest succeeded against an index written by another model, want error")
	}
	if !strings.Contains(err.Error(), "reindex") {
		t.Errorf("error = %v, want it to name the fix (reindex)", err)
	}
	if len(st.upserted) != 0 {
		t.Errorf("wrote %d chunks into a foreign index, want 0", len(st.upserted))
	}
}

func TestIngestor_RefusesLegacyEmbeddingTextFormat(t *testing.T) {
	st := newFakeStore()
	st.identity = "fake/fake-model/4"

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	_, err := ing.Ingest(t.Context(), testSource())
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Fatalf("Ingest error = %v, want an error directing the user to reindex", err)
	}
	if len(st.upserted) != 0 {
		t.Errorf("wrote %d chunks using a mixed embedding-text format, want 0", len(st.upserted))
	}
}

func TestIngestor_AllowsSameEmbeddingModel(t *testing.T) {
	st := newFakeStore()
	st.identity = "fake/fake-model/4/title-heading-v1"

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Errorf("Ingest refused a matching identity: %v", err)
	}
}

func TestIngestor_PropagatesCrawlError(t *testing.T) {
	st := newFakeStore()

	svc, err := ingest.New(
		&fakeCrawler{err: errBoom},
		&fakeParser{},
		newChunker(),
		newFakeEmbedder(),
		chunkSink{st},
	)
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}

	if _, err := svc.Ingest(t.Context(), testSource()); !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want the crawl failure surfaced", err)
	}
}

func TestIngestor_PropagatesStoreError(t *testing.T) {
	st := newFakeStore()
	st.upsertErr = errBoom

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want the store failure surfaced", err)
	}
}

func TestIngestor_PropagatesEmbedderError(t *testing.T) {
	st := newFakeStore()
	emb := newFakeEmbedder()
	emb.err = errBoom

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, emb)

	if _, err := ing.Ingest(t.Context(), testSource()); !errors.Is(err, errBoom) {
		t.Errorf("error = %v, want the embedder failure surfaced", err)
	}
	if len(st.upserted) != 0 {
		t.Errorf("stored %d chunks despite a failed embed", len(st.upserted))
	}
}

func TestIngestor_NoPagesIsNotAnError(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, nil, st, newFakeEmbedder())

	report, err := ing.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("Ingest over an empty site: %v", err)
	}
	if report.PagesDiscovered != 0 {
		t.Errorf("PagesDiscovered = %d, want 0", report.PagesDiscovered)
	}
}

func TestIngestor_ChunksCarryEmbeddings(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, newFakeEmbedder())

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if len(st.embedded) != len(st.upserted) {
		t.Fatalf("stored %d chunks with %d vectors", len(st.upserted), len(st.embedded))
	}
	for i, v := range st.embedded {
		if len(v) != newFakeEmbedder().dim {
			t.Errorf("vector %d has %d dimensions, want %d", i, len(v), newFakeEmbedder().dim)
		}
	}
}

func TestIngestor_ReportCountsAddUp(t *testing.T) {
	st := newFakeStore()
	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
		page("https://docs.acme.test/requests", docB),
	}, st, newFakeEmbedder())

	report, err := ing.Ingest(t.Context(), testSource())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if report.PagesDiscovered != report.PagesFetched+report.PagesSkipped {
		t.Errorf("discovered %d but fetched+skipped = %d; report=%+v",
			report.PagesDiscovered, report.PagesFetched+report.PagesSkipped, report)
	}
	if report.ChunksAdded != len(st.upserted) {
		t.Errorf("ChunksAdded = %d but %d chunks were written", report.ChunksAdded, len(st.upserted))
	}
}

func TestIngestor_StoresChunkWithVectorFromEmbedder(t *testing.T) {
	st := newFakeStore()
	emb := newFakeEmbedder()

	ing := newIngestor(t, []ingest.Page{
		page("https://docs.acme.test/api", docA),
	}, st, emb)

	if _, err := ing.Ingest(t.Context(), testSource()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if len(emb.calls) != 1 || len(emb.calls[0]) != len(st.upserted) {
		t.Fatalf("embedder calls = %#v for %d stored chunks", emb.calls, len(st.upserted))
	}
	for i, saved := range st.upserted {
		for _, part := range []string{saved.Title, saved.HeadingPath, saved.Content} {
			if !strings.Contains(emb.calls[0][i], part) {
				t.Errorf("embedding text %q does not include chunk field %q", emb.calls[0][i], part)
			}
		}
	}

	saved := st.upserted[0]
	got, err := st.GetChunk(t.Context(), saved.ID)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if got.Content != saved.Content {
		t.Error("stored content does not match what was chunked")
	}
	if got.ContentHash == "" {
		t.Error("stored chunk has no content hash")
	}
}

var _ store.Store = (*fakeStore)(nil)

// unknownWidthEmbedder models providers whose width is learned from output.
type unknownWidthEmbedder struct{ *fakeEmbedder }

func (unknownWidthEmbedder) Dimensions() int { return 0 }

func unknownWidthIngestor(t *testing.T, pages []ingest.Page, st *fakeStore, width int) *ingest.Ingestor {
	t.Helper()
	svc, err := ingest.New(&fakeCrawler{pages: pages}, &fakeParser{}, newChunker(),
		unknownWidthEmbedder{&fakeEmbedder{dim: width}}, chunkSink{st})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestIngestor_UnknownWidthPersistsActualIdentityAndKeepsItOnUnchangedSync(t *testing.T) {
	st := newFakeStore()
	pages := []ingest.Page{page("https://docs.acme.test/api", docA)}
	if _, err := unknownWidthIngestor(t, pages, st, 4).Ingest(t.Context(), testSource()); err != nil {
		t.Fatal(err)
	}
	const want = "fake/fake-model/4/title-heading-v1"
	if st.identity != want {
		t.Fatalf("identity = %q, want %q", st.identity, want)
	}
	st.upserted = nil
	if _, err := unknownWidthIngestor(t, pages, st, 6).Ingest(t.Context(), testSource()); err != nil {
		t.Fatal(err)
	}
	if st.identity != want || len(st.upserted) != 0 {
		t.Fatalf("unchanged sync identity = %q, writes = %d", st.identity, len(st.upserted))
	}
}

func TestIngestor_UnknownWidthDriftRefusesAllMutations(t *testing.T) {
	st := newFakeStore()
	pages := []ingest.Page{page("https://docs.acme.test/api", docA), page("https://docs.acme.test/requests", docB)}
	if _, err := unknownWidthIngestor(t, pages, st, 4).Ingest(t.Context(), testSource()); err != nil {
		t.Fatal(err)
	}
	st.upserted = nil
	changed := []ingest.Page{page("https://docs.acme.test/api", "# API\n\nChanged content.")}
	_, err := unknownWidthIngestor(t, changed, st, 6).Ingest(t.Context(), testSource())
	if err == nil || !strings.Contains(err.Error(), "reindex") {
		t.Fatalf("error = %v, want reindex guidance", err)
	}
	if len(st.upserted) != 0 || len(st.deleted) != 0 || st.identity != "fake/fake-model/4/title-heading-v1" {
		t.Fatalf("drift mutated index: writes=%d deletes=%d identity=%q", len(st.upserted), len(st.deleted), st.identity)
	}
}

func TestIngestor_UnknownWidthRefusesUnprovenIdentities(t *testing.T) {
	for _, identity := range []string{
		"fake/fake-model/0/title-heading-v1",
		"fake/fake-model/4",
		"fake/fake-model/4/body-only",
		"fake/foreign-model/4/title-heading-v1",
		"foreign/fake-model/4/title-heading-v1",
	} {
		t.Run(identity, func(t *testing.T) {
			st := newFakeStore()
			st.identity = identity
			_, err := unknownWidthIngestor(t, []ingest.Page{page("https://docs.acme.test/api", docA)}, st, 4).Ingest(t.Context(), testSource())
			if err == nil || !strings.Contains(err.Error(), "reindex") {
				t.Fatalf("error = %v, want reindex guidance", err)
			}
			if len(st.upserted) != 0 || st.identity != identity {
				t.Fatal("refusal mutated index")
			}
		})
	}
}
