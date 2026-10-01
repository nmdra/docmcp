package store_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/store"
)

// Factory builds an empty store that satisfies the Store contract.
type Factory func(t *testing.T) store.Store

// RunStoreContractTests is the shared suite every Store implementation must
// pass: an in-memory fake in the default suite, real Chroma under
// -tags=integration. A contract that only one implementation passes is a bug in
// the contract, not in the implementation.
func RunStoreContractTests(t *testing.T, create Factory) {
	t.Run("UpsertAndGetChunk", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		chunk := store.Chunk{
			ID:          "chunk-1",
			SourceID:    "acme",
			LibraryID:   "/local/acme/1",
			Version:     "1",
			DocumentID:  "doc-1",
			URL:         "https://docs.acme.test/latest/api",
			Title:       "API",
			HeadingPath: "API > Auth",
			Index:       0,
			Content:     "Bearer tokens authenticate every request.",
			ContentHash: "hash-1",
		}

		seed(t, s, []store.Chunk{chunk})

		got, err := s.GetChunk(ctx, "chunk-1")
		if err != nil {
			t.Fatalf("GetChunk: %v", err)
		}
		if got.Content != chunk.Content {
			t.Errorf("Content = %q, want %q", got.Content, chunk.Content)
		}
		if got.LibraryID != "/local/acme/1" || got.SourceID != "acme" {
			t.Errorf("metadata lost: %+v", got)
		}
		if got.HeadingPath != "API > Auth" {
			t.Errorf("HeadingPath = %q", got.HeadingPath)
		}
	})

	t.Run("UpsertReplacesExistingChunk", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		original := store.Chunk{
			ID: "chunk-1", SourceID: "acme", LibraryID: "/local/acme/1",
			Content: "Old content.", ContentHash: "hash-1",
		}
		seed(t, s, []store.Chunk{original})

		edited := original
		edited.Content = "New content."
		edited.ContentHash = "hash-2"

		seed(t, s, []store.Chunk{edited})

		got, err := s.GetChunk(ctx, "chunk-1")
		if err != nil {
			t.Fatalf("GetChunk: %v", err)
		}
		if got.Content != "New content." {
			t.Errorf("Content = %q, want the replacement", got.Content)
		}
		if got.ContentHash != "hash-2" {
			t.Errorf("ContentHash = %q, want hash-2", got.ContentHash)
		}

		count, err := s.CountChunks(ctx, "")
		if err != nil {
			t.Fatalf("CountChunks: %v", err)
		}
		if count != 1 {
			t.Errorf("store holds %d chunks after an upsert of the same ID, want 1", count)
		}
	})

	t.Run("DeleteChunk", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "chunk-1", SourceID: "acme", LibraryID: "/local/acme/1", Content: "keep"},
			{ID: "chunk-2", SourceID: "acme", LibraryID: "/local/acme/1", Content: "drop"},
		})

		if err := s.DeleteChunks(ctx, []string{"chunk-2"}); err != nil {
			t.Fatalf("DeleteChunks: %v", err)
		}

		if _, err := s.GetChunk(ctx, "chunk-2"); !isNotFound(err) {
			t.Errorf("GetChunk after delete error = %v, want not-found", err)
		}
		if _, err := s.GetChunk(ctx, "chunk-1"); err != nil {
			t.Errorf("GetChunk of a kept chunk: %v", err)
		}
	})

	t.Run("DeleteMissingChunkIsNotAnError", func(t *testing.T) {
		s := create(t)

		if err := s.DeleteChunks(t.Context(), []string{"never-existed"}); err != nil {
			t.Errorf("DeleteChunks of a missing ID: %v", err)
		}
	})

	t.Run("FilterByLibrary", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "a1", SourceID: "acme", LibraryID: "/local/acme/1", Content: "acme one"},
			{ID: "a2", SourceID: "acme", LibraryID: "/local/acme/1", Content: "acme one more"},
			{ID: "b1", SourceID: "other", LibraryID: "/local/other/1", Content: "other lib"},
		})

		chunks, err := s.ListChunks(ctx, store.ListFilter{LibraryID: "/local/acme/1"})
		if err != nil {
			t.Fatalf("ListChunks: %v", err)
		}
		if len(chunks) != 2 {
			t.Errorf("ListChunks returned %d chunks, want 2 for /local/acme/1", len(chunks))
		}
		for _, c := range chunks {
			if c.LibraryID != "/local/acme/1" {
				t.Errorf("chunk %q leaked from library %q", c.ID, c.LibraryID)
			}
		}
	})

	t.Run("FilterByVersion", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "v1", SourceID: "acme", LibraryID: "/local/acme/1", Version: "1", Content: "old"},
			{ID: "v2", SourceID: "acme", LibraryID: "/local/acme/1", Version: "2", Content: "new"},
		})

		chunks, err := s.ListChunks(ctx, store.ListFilter{LibraryID: "/local/acme/1", Version: "1"})
		if err != nil {
			t.Fatalf("ListChunks: %v", err)
		}
		if len(chunks) != 1 || chunks[0].ID != "v1" {
			t.Fatalf("ListChunks = %v, want only the v1 chunk", ids(chunks))
		}
	})

	t.Run("FilterBySource", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "a1", SourceID: "acme", LibraryID: "/local/acme/1", Content: "a"},
			{ID: "b1", SourceID: "other", LibraryID: "/local/other/1", Content: "b"},
		})

		chunks, err := s.ListChunks(ctx, store.ListFilter{SourceID: "other"})
		if err != nil {
			t.Fatalf("ListChunks: %v", err)
		}
		if len(chunks) != 1 || chunks[0].ID != "b1" {
			t.Errorf("ListChunks = %v, want only the other source", ids(chunks))
		}
	})

	t.Run("DeleteSourceChunks", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "a1", SourceID: "acme", LibraryID: "/local/acme/1", Content: "a"},
			{ID: "a2", SourceID: "acme", LibraryID: "/local/acme/1", Content: "b"},
			{ID: "b1", SourceID: "other", LibraryID: "/local/other/1", Content: "c"},
		})

		if err := s.DeleteSourceChunks(ctx, "acme"); err != nil {
			t.Fatalf("DeleteSourceChunks: %v", err)
		}

		chunks, err := s.ListChunks(ctx, store.ListFilter{SourceID: "acme"})
		if err != nil {
			t.Fatalf("ListChunks: %v", err)
		}
		if len(chunks) != 0 {
			t.Errorf("%d chunks survived a source delete: %v", len(chunks), ids(chunks))
		}

		others, err := s.ListChunks(ctx, store.ListFilter{SourceID: "other"})
		if err != nil {
			t.Fatalf("ListChunks for other: %v", err)
		}
		if len(others) != 1 {
			t.Error("deleting one source removed another source's chunks")
		}
	})

	t.Run("QueryReturnsRelevantChunks", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "auth", SourceID: "acme", LibraryID: "/local/acme/1",
				Content: "Bearer tokens authenticate every request."},
			{ID: "routing", SourceID: "acme", LibraryID: "/local/acme/1",
				Content: "Routes map request paths to handlers."},
			{ID: "cache", SourceID: "acme", LibraryID: "/local/acme/1",
				Content: "The cache evicts entries after their TTL expires."},
		})

		// The query vector is the target chunk's own embedding, so the nearest
		// hit is unambiguous whatever distance metric the store uses.
		results, err := s.Query(ctx, store.Query{
			LibraryID: "/local/acme/1",
			Embedding: testVector("Bearer tokens authenticate every request."),
			TopK:      3,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("Query returned nothing")
		}
		if results[0].Chunk.ID != "auth" {
			t.Errorf("top hit = %q, want the nearest chunk by embedding distance", results[0].Chunk.ID)
		}
	})

	t.Run("QueryRespectsLibraryFilter", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		vector := testVector("same text")
		seed(t, s, []store.Chunk{
			{ID: "mine", SourceID: "acme", LibraryID: "/local/acme/1",
				Content: "same text", Embedding: vector},
			{ID: "theirs", SourceID: "other", LibraryID: "/local/other/1",
				Content: "same text", Embedding: vector},
		})

		results, err := s.Query(ctx, store.Query{
			LibraryID: "/local/acme/1",
			Embedding: vector,
			TopK:      5,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		for _, r := range results {
			if r.Chunk.LibraryID != "/local/acme/1" {
				t.Errorf("query leaked a chunk from %q", r.Chunk.LibraryID)
			}
		}
	})

	t.Run("QueryHonorsTopK", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		var chunks []store.Chunk
		for i := range 10 {
			chunks = append(chunks, store.Chunk{
				ID:        "c" + string(rune('a'+i)),
				SourceID:  "acme",
				LibraryID: "/local/acme/1",
				Content:   "chunk",
				Embedding: testVector(fmt.Sprintf("chunk %d", i)),
			})
		}
		seed(t, s, chunks)

		results, err := s.Query(ctx, store.Query{
			LibraryID: "/local/acme/1",
			Embedding: testVector("chunk 5"),
			TopK:      3,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(results) != 3 {
			t.Errorf("Query returned %d results for TopK 3, want 3", len(results))
		}
	})

	t.Run("QueryWithNoMatches", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		results, err := s.Query(ctx, store.Query{
			LibraryID: "/local/nothing/9",
			Embedding: testVector("anything"),
			TopK:      5,
		})
		if err != nil {
			t.Fatalf("Query on an empty library: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Query returned %d results for an unknown library, want 0", len(results))
		}
	})

	t.Run("CountChunks", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "a", SourceID: "acme", LibraryID: "/local/acme/1", Content: "a"},
			{ID: "b", SourceID: "acme", LibraryID: "/local/acme/1", Content: "b"},
			{ID: "c", SourceID: "other", LibraryID: "/local/other/1", Content: "c"},
		})

		all, err := s.CountChunks(ctx, "")
		if err != nil {
			t.Fatalf("CountChunks: %v", err)
		}
		if all != 3 {
			t.Errorf("CountChunks(all) = %d, want 3", all)
		}

		scoped, err := s.CountChunks(ctx, "/local/acme/1")
		if err != nil {
			t.Fatalf("CountChunks scoped: %v", err)
		}
		if scoped != 2 {
			t.Errorf("CountChunks(/local/acme/1) = %d, want 2", scoped)
		}
	})

	t.Run("IdentityRoundTrips", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		const identity = "ollama/nomic-embed-text/768"

		if err := s.SetIdentity(ctx, identity); err != nil {
			t.Fatalf("SetIdentity: %v", err)
		}
		got, err := s.Identity(ctx)
		if err != nil {
			t.Fatalf("Identity: %v", err)
		}
		if got != identity {
			t.Errorf("Identity() = %q, want %q", got, identity)
		}
	})

	t.Run("IdentityOfEmptyStoreIsEmpty", func(t *testing.T) {
		s := create(t)

		got, err := s.Identity(t.Context())
		if err != nil {
			t.Fatalf("Identity on empty store: %v", err)
		}
		if got != "" {
			t.Errorf("Identity() = %q, want empty for a fresh index", got)
		}
	})

	t.Run("EmptyUpsertIsNotAnError", func(t *testing.T) {
		s := create(t)

		if err := s.UpsertChunks(t.Context(), nil, nil); err != nil {
			t.Errorf("UpsertChunks(nil): %v", err)
		}
		if err := s.DeleteChunks(t.Context(), nil); err != nil {
			t.Errorf("DeleteChunks(nil): %v", err)
		}
	})

	t.Run("RejectsChunkWithoutID", func(t *testing.T) {
		s := create(t)

		if err := s.UpsertChunks(t.Context(), []store.Chunk{{Content: "no id"}}, nil); err == nil {
			t.Error("UpsertChunks accepted a chunk with no ID, want error")
		}
	})

	t.Run("ListChunksIsOrdered", func(t *testing.T) {
		s := create(t)
		ctx := t.Context()

		seed(t, s, []store.Chunk{
			{ID: "z", SourceID: "acme", LibraryID: "/local/acme/1", Content: "z"},
			{ID: "a", SourceID: "acme", LibraryID: "/local/acme/1", Content: "a"},
			{ID: "m", SourceID: "acme", LibraryID: "/local/acme/1", Content: "m"},
		})

		for range 3 {
			chunks, err := s.ListChunks(ctx, store.ListFilter{LibraryID: "/local/acme/1"})
			if err != nil {
				t.Fatalf("ListChunks: %v", err)
			}
			if len(chunks) != 3 || chunks[0].ID != "a" || chunks[2].ID != "z" {
				t.Fatalf("ListChunks not ordered by ID: %v", ids(chunks))
			}
		}
	})
}

// PersistAcrossRestart is a separate check because it needs two store instances
// sharing one directory; an in-memory fake legitimately cannot satisfy it.
func PersistAcrossRestart(t *testing.T, create func(t *testing.T) store.Store) {
	ctx := t.Context()

	chunk := store.Chunk{
		ID: "persist-1", SourceID: "acme", LibraryID: "/local/acme/1",
		Content: "survives a restart", ContentHash: "hash-p",
	}

	first := create(t)
	seed(t, first, []store.Chunk{chunk})
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := create(t)
	t.Cleanup(func() { second.Close() })

	got, err := second.GetChunk(ctx, "persist-1")
	if err != nil {
		t.Fatalf("GetChunk after reopen: %v", err)
	}
	if got.Content != chunk.Content {
		t.Errorf("Content = %q, want %q", got.Content, chunk.Content)
	}
}

// seed stores chunks with a deterministic vector each. A vector store cannot
// hold an unembedded chunk, so seeding without embeddings is not a case the
// contract can describe.
func seed(t *testing.T, s store.Store, chunks []store.Chunk) {
	t.Helper()

	ctx := t.Context()

	vectors := make([][]float32, len(chunks))
	for i, c := range chunks {
		if len(c.Embedding) == 0 {
			vectors[i] = testVector(c.Content)
			continue
		}
		vectors[i] = c.Embedding
	}

	if err := s.UpsertChunks(ctx, chunks, vectors); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
}

// testVector maps text onto a stable unit-ish vector: identical text produces an
// identical vector, and different text produces a different one, which is what
// ranking assertions need.
// testVectorDim is fixed so every fixture in the contract shares one space.
const testVectorDim = 8

func testVector(text string) []float32 {
	v := make([]float32, testVectorDim)

	var sum int
	for _, r := range text {
		sum += int(r)
	}
	for i := range v {
		v[i] = float32((sum*(i+1))%97) / 97
	}
	v[0] += 1 // keep the vector non-zero so cosine distance is defined
	return v
}

func ids(chunks []store.Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.ID)
	}
	sort.Strings(out)
	return out
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}
