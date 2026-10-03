package search_test

import (
	"testing"

	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/source"
)

func libraries() []source.Source {
	return []source.Source{
		{ID: "1", LibraryID: "/local/pi", Name: "Pi", Description: "Pi coding agent documentation",
			Version: "", BaseURL: "https://pi.dev/docs/"},
		{ID: "2", LibraryID: "/local/pi/0.99.2", Name: "Pi", Description: "Pi coding agent documentation",
			Version: "0.99.2", BaseURL: "https://pi.dev/docs/"},
		{ID: "3", LibraryID: "/local/pi/0.99.1", Name: "Pi", Description: "Pi coding agent documentation",
			Version: "0.99.1", BaseURL: "https://pi.dev/docs/"},
		{ID: "4", LibraryID: "/local/pi-sdk", Name: "Pi SDK", Description: "Pi SDK reference",
			Version: "", BaseURL: "https://pi.dev/sdk/"},
		{ID: "5", LibraryID: "/local/pico-css", Name: "Pico CSS", Description: "A tiny CSS framework",
			Version: "", BaseURL: "https://pico.test/"},
	}
}

func resolve(t *testing.T, name, query string) []search.Match {
	t.Helper()

	r := search.NewResolver(libraries())
	matches, err := r.Resolve(t.Context(), name, query)
	if err != nil {
		t.Fatalf("Resolve(%q, %q): %v", name, query, err)
	}
	return matches
}

func ids(matches []search.Match) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.LibraryID)
	}
	return out
}

func TestResolver_ExactNameWins(t *testing.T) {
	matches := resolve(t, "Pi", "codemode MCP exposure")

	if len(matches) == 0 {
		t.Fatal("no matches for an exact name")
	}
	if matches[0].Name != "Pi" {
		t.Errorf("top match = %q, want the exact name Pi", matches[0].Name)
	}
	if matches[0].Version != "" {
		t.Errorf("top match version = %q, want the unversioned library", matches[0].Version)
	}
}

func TestResolver_CaseInsensitive(t *testing.T) {
	matches := resolve(t, "pi", "exposure")

	if len(matches) == 0 {
		t.Fatal("no matches for a lowercase name")
	}
	if matches[0].Name != "Pi" {
		t.Errorf("top match = %q, want Pi", matches[0].Name)
	}
}

func TestResolver_NormalizedName(t *testing.T) {
	for _, name := range []string{"Pico CSS", "pico-css", "pico_css", "PicoCSS"} {
		matches := resolve(t, name, "styling")
		if len(matches) == 0 {
			t.Errorf("no match for %q", name)
			continue
		}
		if matches[0].Name != "Pico CSS" {
			t.Errorf("for %q top match = %q, want Pico CSS", name, matches[0].Name)
		}
	}
}

func TestResolver_UsesQueryForAmbiguousLibraries(t *testing.T) {
	matches := resolve(t, "Pi", "sdk client reference")

	if len(matches) == 0 {
		t.Fatal("no matches")
	}
	// "Pi" must beat "Pi SDK" even though the query mentions sdk, because the
	// name is a stronger signal than query tokens.
	if matches[0].Name != "Pi" {
		t.Errorf("top match = %q, want the exact name to beat query overlap", matches[0].Name)
	}
}

func TestResolver_ReturnsVersions(t *testing.T) {
	matches := resolve(t, "Pi", "exposure")

	var versions []string
	for _, m := range matches {
		if m.Name == "Pi" && m.Version != "" {
			versions = append(versions, m.Version)
		}
	}
	if len(versions) == 0 {
		t.Fatalf("no versioned Pi libraries returned: %v", ids(matches))
	}
}

func TestResolver_VersionMentionRanksVersion(t *testing.T) {
	matches := resolve(t, "Pi", "use version 0.99.1 features")

	found := false
	for _, m := range matches {
		if m.Version == "0.99.1" {
			found = true
		}
	}
	if !found {
		t.Errorf("the mentioned version 0.99.1 was not returned: %v", ids(matches))
	}
	if len(matches) > 0 && matches[0].Version != "0.99.1" {
		t.Errorf("top match version = %q, want the version the query names", matches[0].Version)
	}
}

func TestResolver_NoMatch(t *testing.T) {
	matches := resolve(t, "Completely Unknown Library", "anything")

	for _, m := range matches {
		if m.Name == "Pico CSS" {
			t.Errorf("unrelated library %q returned for an unknown name", m.LibraryID)
		}
	}
}

func TestResolver_EmptyNameReturnsError(t *testing.T) {
	r := search.NewResolver(libraries())

	if _, err := r.Resolve(t.Context(), "  ", "query"); err == nil {
		t.Error("Resolve with an empty name succeeded, want error")
	}
}

func TestResolver_NeverReturnsDuplicateLibraryIDs(t *testing.T) {
	matches := resolve(t, "Pi", "exposure")

	seen := map[string]bool{}
	for _, m := range matches {
		if seen[m.LibraryID] {
			t.Errorf("duplicate library ID %q", m.LibraryID)
		}
		seen[m.LibraryID] = true
	}
}

func TestResolver_ExactLibraryIDResolvesDirectly(t *testing.T) {
	matches := resolve(t, "/local/pi/0.99.2", "exposure")

	if len(matches) != 1 {
		t.Fatalf("got %d matches, want exactly the requested library: %v", len(matches), ids(matches))
	}
	if matches[0].LibraryID != "/local/pi/0.99.2" {
		t.Errorf("resolved %q, want /local/pi/0.99.2", matches[0].LibraryID)
	}
}

func TestResolver_MatchCarriesDescription(t *testing.T) {
	matches := resolve(t, "Pi SDK", "client")

	if len(matches) == 0 {
		t.Fatal("no matches")
	}
	if matches[0].Description == "" {
		t.Error("match carries no description for resolve-library-id to show")
	}
}

func TestResolver_SkipsUnindexedLibraries(t *testing.T) {
	// A library with no chunks should not be offered: an agent that resolves to
	// it and gets nothing back has been misled.
	list := []source.Source{
		{ID: "1", LibraryID: "/local/empty", Name: "Empty", Description: "nothing indexed"},
		{ID: "2", LibraryID: "/local/full", Name: "Full", Description: "indexed"},
	}

	r := search.NewResolver(list)
	r.SetIndexed(map[string]int{"/local/full": 12})

	matches, err := r.Resolve(t.Context(), "Empty", "query")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, m := range matches {
		if m.LibraryID == "/local/empty" {
			t.Error("an unindexed library was offered to the agent")
		}
	}
}

func TestResolver_RankIsDeterministic(t *testing.T) {
	first := ids(resolve(t, "Pi", "exposure"))
	for range 5 {
		got := ids(resolve(t, "Pi", "exposure"))
		for i := range first {
			if got[i] != first[i] {
				t.Fatalf("ranking changed between runs: %v vs %v", first, got)
			}
		}
	}
}
