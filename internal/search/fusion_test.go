package search_test

import (
	"reflect"
	"testing"

	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/store"
)

func fusionCandidates(ids ...string) []search.Result {
	out := make([]search.Result, len(ids))
	for i, id := range ids {
		out[i] = search.Result{Chunk: store.Chunk{ID: id}}
	}
	return out
}

func TestRRF_ResultInBothListsRanksHigher(t *testing.T) {
	got := search.RRF(fusionCandidates("a", "b", "c"), fusionCandidates("c", "d", "a"))
	want := []string{"a", "c", "b", "d"}
	var ids []string
	for _, result := range got {
		ids = append(ids, result.Chunk.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("IDs = %v, want %v", ids, want)
	}
	if score := search.RRFScore(1, 60); score < 0.01639344 || score > 0.01639345 {
		t.Fatalf("rank-one score = %v", score)
	}
}

func TestRRF_MissingCandidateHandled(t *testing.T) {
	got := search.RRF(nil, fusionCandidates("only"))
	if len(got) != 1 || got[0].Chunk.ID != "only" {
		t.Fatalf("results = %+v", got)
	}
	if got := search.RRF(nil, nil); len(got) != 0 {
		t.Fatalf("empty lists = %+v", got)
	}
}

func TestRRF_TieBreakStable(t *testing.T) {
	got := search.RRF(fusionCandidates("z"), fusionCandidates("a"))
	if len(got) != 2 || got[0].Chunk.ID != "a" || got[1].Chunk.ID != "z" {
		t.Fatalf("tie order = %+v", got)
	}
}

func TestRRF_Deterministic(t *testing.T) {
	dense := fusionCandidates("z", "b", "a")
	lexical := fusionCandidates("a", "c", "z")
	want := search.RRF(dense, lexical)
	for i := 0; i < 100; i++ {
		if got := search.RRF(dense, lexical); !reflect.DeepEqual(got, want) {
			t.Fatalf("unstable result: %+v, want %+v", got, want)
		}
	}
}

func TestRRF_DuplicateIDContributesOncePerList(t *testing.T) {
	got := search.RRF(fusionCandidates("a", "a"), fusionCandidates("z"))
	if len(got) != 2 || got[0].Score != got[1].Score {
		t.Fatalf("duplicate inflated score: %+v", got)
	}
}
