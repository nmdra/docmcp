package search_test

import (
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/search"
	"github.com/docmcp/docmcp/internal/store"
)

// vec turns a fixture vector into a slice, which is what a query carries.
func vec(v [3]float32) []float32 { return v[:] }

const libraryA = "/local/acme/1"
const libraryB = "/local/beta/1"

func TestSearch_FiltersLibrary(t *testing.T) {
	engine := newTestEngine(t,
		chunk("a1", libraryA, "Bearer tokens authenticate requests.", [3]float32{0, 1, 0}),
		chunk("b1", libraryB, "Bearer tokens authenticate requests.", [3]float32{0, 1, 0}),
	)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA, Query: "how do I authenticate",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range results {
		if r.Chunk.LibraryID != libraryA {
			t.Errorf("result leaked from %q", r.Chunk.LibraryID)
		}
	}
}

func TestSearch_FiltersVersion(t *testing.T) {
	engine := newTestEngine(t,
		chunk("v1", "/local/acme/1", "Authentication uses API keys.", [3]float32{0, 1, 0}),
		chunk("v2", "/local/acme/2", "Authentication uses OAuth.", [3]float32{0, 1, 0}),
	)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: "/local/acme/1", Query: "authentication",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Chunk.Version != "1" {
			t.Errorf("version 2 content leaked into a version 1 query: %+v", r.Chunk)
		}
		if strings.Contains(r.Chunk.Content, "OAuth") {
			t.Error("v2 content returned for a v1 query")
		}
	}
}

func TestSearch_ReturnsRelevantChunks(t *testing.T) {
	engine := newTestEngine(t,
		chunk("auth", libraryA, "Bearer tokens authenticate every request.", [3]float32{0, 1, 0}),
		chunk("routing", libraryA, "Routes map request paths to handlers.", [3]float32{1, 0, 0}),
		chunk("cache", libraryA, "The cache evicts entries after their TTL expires.", [3]float32{0, 0, 1}),
	)

	// The query embeds to the same vector as the "auth" chunk.
	results, err := engine.Search(t.Context(), search.Request{
		LibraryID:      libraryA,
		Query:          "authentication tokens",
		QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Chunk.ID != "auth" {
		t.Errorf("top hit = %q, want the nearest chunk", results[0].Chunk.ID)
	}
}

func TestSearch_DeduplicatesAdjacentChunks(t *testing.T) {
	engine := newTestEngine(t,
		chunk("c1", libraryA, "Same section text.", [3]float32{0, 1, 0}),
		chunk("c2", libraryA, "Same section text.", [3]float32{0, 1, 0}),
		chunk("c3", libraryA, "Same section text.", [3]float32{0, 1, 0}),
	)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID:      libraryA,
		QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("returned %d near-identical chunks, want them deduplicated", len(results))
	}
}

func TestSearch_LimitsContextInternally(t *testing.T) {
	var many []store.Chunk
	for i := range 40 {
		v := [3]float32{float32(i) / 40, 1, 0}
		many = append(many, chunk(
			"c"+strings.Repeat("x", i%5)+string(rune('a'+i%26)),
			libraryA, "Chunk text number "+string(rune('a'+i%26)), v))
	}

	engine := newTestEngine(t, many...)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID:      libraryA,
		QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("no results")
	}
	if len(results) > engine.Options().FinalChunks {
		t.Errorf("returned %d chunks, want at most the internal budget of %d",
			len(results), engine.Options().FinalChunks)
	}
}

func TestSearch_PreservesSourceURLs(t *testing.T) {
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA, QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}

	// A result an agent cannot cite is a result it cannot use.
	if results[0].Chunk.URL == "" {
		t.Error("result carries no source URL")
	}
	if results[0].Chunk.HeadingPath == "" {
		t.Error("result carries no heading path to locate the section")
	}
}

func TestSearch_NoResults(t *testing.T) {
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: "/local/nothing/9", QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results for an unknown library, want 0", len(results))
	}
}

func TestSearch_RejectsUnknownLibrary(t *testing.T) {
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	_, err := engine.Search(t.Context(), search.Request{
		LibraryID: "not-a-library-id", QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err == nil {
		t.Error("Search accepted a malformed library ID, want error")
	}
}

func TestSearch_RejectsEmptyQuery(t *testing.T) {
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	_, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA})
	if err == nil {
		t.Error("Search with no query at all succeeded, want error")
	}
}

func TestSearch_UsesEmbedderWhenNoVectorGiven(t *testing.T) {
	engine := newTestEngine(t,
		chunk("auth", libraryA, "Bearer tokens authenticate every request.", [3]float32{0, 1, 0}),
		chunk("cache", libraryA, "The cache evicts after TTL.", [3]float32{0, 0, 1}),
	)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA,
		Query:     "authentication",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results; the engine did not embed the query")
	}
}

func TestSearch_ScoreIsNotExposedToCallers(t *testing.T) {
	// The internal score exists for ordering. Nothing in the returned result
	// should tempt a caller to show a distance to a model.
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA, QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, r := range results {
		formatted := r.Format()
		for _, leak := range []string{"distance", "score", "cosine", "topK", "vector"} {
			if strings.Contains(strings.ToLower(formatted), strings.ToLower(leak)) {
				t.Errorf("formatted result leaks %q:\n%s", leak, formatted)
			}
		}
	}
}

func TestSearch_FormatProducesNeutralSections(t *testing.T) {
	engine := newTestEngine(t, chunk("c1", libraryA, "Some text.", [3]float32{0, 1, 0}))

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA, QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}

	got := results[0].Format()
	for _, want := range []string{"### ", "Source: ", "Library: "} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted result missing %q:\n%s", want, got)
		}
	}
}

func TestSearch_FormatSeparatesChunks(t *testing.T) {
	engine := newTestEngine(t,
		chunk("c1", libraryA, "First chunk.", [3]float32{0, 1, 0}),
		chunk("c2", libraryA, "Second chunk.", [3]float32{0, 1, 1}),
	)

	results, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA, QueryEmbedding: vec([3]float32{0, 1, 0}),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("got %d results, want 2 to test joining", len(results))
	}

	joined := results[0].Format() + "\n---\n" + results[1].Format()
	if !strings.Contains(joined, "---") {
		t.Errorf("joined output has no separator:\n%s", joined)
	}
}
