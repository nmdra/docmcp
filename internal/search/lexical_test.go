package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/search"
	"github.com/docmcp/docmcp/internal/store"
)

func TestSearch_AcronymQueryFindsDirectAnswerWithinResultBudget(t *testing.T) {
	target := chunk(
		"z-direct-exposure", libraryA,
		"Direct exposure declares an MCP tool to the model like a built-in tool.",
		[3]float32{1, 0, 0},
	)
	target.Title = "Pi MCP documentation"
	target.HeadingPath = "Control tool exposure"

	chunks := []store.Chunk{target}
	for _, id := range []string{
		"a-overview", "b-setup", "c-oauth", "d-transport",
		"e-resources", "f-extensions", "g-permissions", "h-commands",
	} {
		chunks = append(chunks, chunk(id, libraryA,
			"General documentation about configuring a server: "+id, [3]float32{0, 1, 0}))
	}
	engine := newTestEngine(t, chunks...)

	got, err := engine.Search(t.Context(), search.Request{
		LibraryID: libraryA,
		Query:     "How does direct MCP exposure work?",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range got {
		if result.Chunk.ID == target.ID {
			return
		}
	}
	t.Fatalf("direct-exposure answer is absent from the normal result budget: %+v", got)
}

func TestSearch_LexicalFindsTechnicalIdentifiersOutsideDenseCandidates(t *testing.T) {
	for _, identifier := range []string{"oauth.clientName", "defaultTools", "query-docs", "resolve-library-id", "/reload"} {
		t.Run(identifier, func(t *testing.T) {
			dense := chunk("a-dense", libraryA, "An unrelated semantic candidate.", [3]float32{0, 1, 0})
			exact := chunk("z-exact", libraryA, "Configure `"+identifier+"` here.", [3]float32{1, 0, 0})
			engine, err := search.NewEngine(newMemoryStore(dense, exact), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: identifier})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[1].Chunk.ID != exact.ID {
				t.Fatalf("results = %+v, want dense and lexical-only candidate", got)
			}
		})
	}
}

// listingStore models independent dense and listing operations at the Store boundary.
type listingStore struct {
	store.Store
	chunks []store.Chunk
	err    error
}

func (s *listingStore) ListChunks(context.Context, store.ListFilter) ([]store.Chunk, error) {
	return s.chunks, s.err
}

func TestSearch_LexicalFailureIsReported(t *testing.T) {
	for _, query := range []string{"codeMode", "How does HTTP work?"} {
		t.Run(query, func(t *testing.T) {
			failure := errors.New("listing failed")
			st := &listingStore{Store: newMemoryStore(), err: failure}
			engine, err := search.NewEngine(st, &fixedEmbedder{}, search.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: query})
			if !errors.Is(err, failure) || !strings.Contains(err.Error(), "lexical") {
				t.Fatalf("error = %v, want lexical listing failure", err)
			}
		})
	}
}

func TestSearch_LexicalSplitsCamelCaseIdentifiers(t *testing.T) {
	dense := chunk("a-dense", libraryA, "Unrelated semantic match.", [3]float32{0, 1, 0})
	exact := chunk("z-exact", libraryA, "Set oauth.clientName to identify the application.", [3]float32{1, 0, 0})
	engine, err := search.NewEngine(newMemoryStore(dense, exact), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: "client-name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Chunk.ID != exact.ID {
		t.Fatalf("results = %+v, want camel-case identifier match", got)
	}
}

func TestSearch_QueryEmbeddingOnlyKeepsDenseScoresAndSkipsLexicalListing(t *testing.T) {
	dense := chunk("dense", libraryA, "Dense content.", [3]float32{0, 1, 0})
	st := &listingStore{Store: newMemoryStore(dense), err: errors.New("must not list")}
	engine, err := search.NewEngine(st, &fixedEmbedder{}, search.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, QueryEmbedding: []float32{0, 1, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Chunk.ID != "dense" || got[0].Score != 0 {
		t.Fatalf("dense-only results = %+v", got)
	}
}

func TestSearch_LexicalMatchesMetadataAndScopesLibraryVersion(t *testing.T) {
	for _, field := range []string{"title", "heading"} {
		t.Run(field, func(t *testing.T) {
			dense := chunk("a-dense", libraryA, "Unrelated semantic candidate.", [3]float32{0, 1, 0})
			exact := chunk("z-exact", libraryA, "Configuration details.", [3]float32{1, 0, 0})
			if field == "title" {
				exact.Title = "defaultTools"
			} else {
				exact.HeadingPath = "Settings > defaultTools"
			}
			foreign := chunk("foreign", libraryB, "defaultTools", [3]float32{1, 0, 0})
			version := chunk("version", "/local/acme/2", "defaultTools defaultTools", [3]float32{1, 0, 0})
			conflictingVersion := chunk("conflicting-version", libraryA, "defaultTools defaultTools defaultTools", [3]float32{1, 0, 0})
			conflictingVersion.Version = "2"
			engine, err := search.NewEngine(newMemoryStore(dense, exact, foreign, version, conflictingVersion), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: "defaultTools"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[1].Chunk.ID != exact.ID {
				t.Fatalf("scoped metadata results = %+v", got)
			}
		})
	}
}

func TestSearch_LexicalExactIdentifierBeatsPartialRepetitionAndBreaksTiesByID(t *testing.T) {
	dense := chunk("dense", libraryA, "Unrelated semantics.", [3]float32{0, 1, 0})
	a := chunk("a-exact", libraryA, "oauth.clientName", [3]float32{1, 0, 0})
	z := chunk("z-exact", libraryA, "oauth.clientName", [3]float32{1, 0, 0})
	repeated := chunk("repeated", libraryA, strings.Repeat("oauth ", 100), [3]float32{1, 0, 0})
	substring := chunk("substring", libraryA, "xoauth.clientNameSuffix", [3]float32{1, 0, 0})
	engine, err := search.NewEngine(newMemoryStore(dense, a, z, repeated, substring), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: "oauth.clientName"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Chunk.ID != a.ID {
			t.Fatalf("overlap/tie result = %+v", got)
		}
	}
}

func TestSearch_LexicalRareIdentifierOutranksCommonLanguageOverlap(t *testing.T) {
	common := chunk("z-common", libraryA, "How do I configure the application?", [3]float32{0, 1, 0})
	rare := chunk("a-rare", libraryA, "codeMode", [3]float32{1, 0, 0})
	chunks := []store.Chunk{common, rare}
	for _, id := range []string{"filler-b", "filler-c", "filler-d", "filler-e", "filler-f", "filler-g", "filler-h", "filler-i"} {
		chunks = append(chunks, chunk(id, libraryA, "How do I configure another application? "+id, [3]float32{1, 0, 0}))
	}
	engine, err := search.NewEngine(newMemoryStore(chunks...), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: "How do I configure codeMode?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Chunk.ID != rare.ID {
		t.Fatalf("results = %+v, want rare identifier before common-language overlap", got)
	}
}

func TestSearch_OrdinaryLanguageKeepsDenseRankingAndSkipsLexicalListing(t *testing.T) {
	for _, query := range []string{"How do I authenticate?", "What commands are available.", "codemode", "CLIENT NAME"} {
		t.Run(query, func(t *testing.T) {
			dense := chunk("dense", libraryA, "Semantic content.", [3]float32{0, 1, 0})
			st := &listingStore{Store: newMemoryStore(dense), err: errors.New("ordinary query must not list")}
			engine, err := search.NewEngine(st, &fixedEmbedder{}, search.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Chunk.ID != dense.ID || got[0].Score != 0 {
				t.Fatalf("dense results = %+v", got)
			}
		})
	}
}

func TestSearch_LexicalShortCommandDocumentationOutranksLongIncidentalMention(t *testing.T) {
	dense := chunk("z-dense", libraryA, "Semantic introduction.", [3]float32{0, 1, 0})
	short := chunk("b-command", libraryA, "/refresh reloads project configuration.", [3]float32{1, 0, 0})
	long := chunk("a-incidental", libraryA, "This guide mentions /refresh once. "+strings.Repeat("Background concepts and unrelated application examples. ", 100), [3]float32{1, 0, 0})
	engine, err := search.NewEngine(newMemoryStore(dense, short, long), &fixedEmbedder{}, search.Options{CandidateCount: 1, FinalChunks: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Search(t.Context(), search.Request{LibraryID: libraryA, Query: "/refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Chunk.ID != short.ID {
		t.Fatalf("results = %+v, want concise command documentation before incidental prose", got)
	}
}

// cancelAfterListingStore returns valid local candidates but cancels the request
// just before returning, so the engine must not report a successful result.
type cancelAfterListingStore struct {
	store.Store
	cancel context.CancelFunc
}

func (s *cancelAfterListingStore) ListChunks(ctx context.Context, filter store.ListFilter) ([]store.Chunk, error) {
	chunks, err := s.Store.ListChunks(ctx, filter)
	s.cancel()
	return chunks, err
}

func TestSearch_CancellationAfterLexicalListingIsReported(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching corpus", true: "empty corpus"}[empty], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			chunks := []store.Chunk{chunk("exact", libraryA, "Configure defaultTools.", [3]float32{0, 1, 0})}
			if empty {
				chunks = nil
			}
			st := &cancelAfterListingStore{Store: newMemoryStore(chunks...), cancel: cancel}
			engine, err := search.NewEngine(st, &fixedEmbedder{}, search.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.Search(ctx, search.Request{LibraryID: libraryA, Query: "defaultTools"})
			if !errors.Is(err, context.Canceled) || len(got) != 0 {
				t.Fatalf("Search = %+v, %v, want no results and context.Canceled", got, err)
			}
		})
	}
}
