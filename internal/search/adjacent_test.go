package search_test

import (
	"fmt"
	"testing"

	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/store"
)

func TestSearch_DuplicateChunkIndicesBypassStructuralSuppression(t *testing.T) {
	for _, indexes := range [][]int{{0, 0, 0}, {0, 1, 1}, {1, 1, 2}} {
		t.Run(fmt.Sprint(indexes), func(t *testing.T) {
			var chunks []store.Chunk
			for i, index := range indexes {
				c := chunk(fmt.Sprintf("ambiguous%d", i), "/local/acme/1", fmt.Sprintf("Unique content %d", i), [3]float32{0, 1, 0})
				c.DocumentID = "shared-document"
				c.HeadingPath = "Guide > Shared section"
				c.Index = index
				chunks = append(chunks, c)
			}
			chunks = append(chunks, chunk("distinct", "/local/acme/1", "Distinct lower ranked content", [3]float32{1, 0, 0}))
			engine, err := search.NewEngine(newMemoryStore(chunks...), &fixedEmbedder{}, search.Options{CandidateCount: 20, FinalChunks: 3})
			if err != nil {
				t.Fatal(err)
			}
			results, err := engine.Search(t.Context(), search.Request{LibraryID: "/local/acme/1", QueryEmbedding: []float32{0, 1, 0}})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 3 {
				t.Fatalf("got %d results, want 3", len(results))
			}
			for i, result := range results {
				if result.Chunk.ID != chunks[i].ID {
					t.Fatalf("rank %d: got %s, want %s; ambiguous indices must bypass suppression", i+1, result.Chunk.ID, chunks[i].ID)
				}
			}
		})
	}
}
