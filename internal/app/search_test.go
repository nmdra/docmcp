package app_test

import (
	"strings"
	"testing"
)

func TestSearchCommand_ShowsResults(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "search", "--library", "/local/fixture/1", "authentication token exchange")
	if err != nil {
		t.Fatalf("search: %v\n%s", err, out)
	}

	if !strings.Contains(out, "Source:") {
		t.Errorf("search output has no source URL:\n%s", out)
	}
	if !strings.Contains(out, "/local/fixture/1") {
		t.Errorf("search output does not name the library:\n%s", out)
	}
}

func TestSearchCommand_RequiresQuery(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "search", "--library", "/local/x/1"); err == nil {
		t.Error("search with no query succeeded, want error")
	}
}

func TestSearchCommand_RequiresLibrary(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "search", "authentication"); err == nil {
		t.Error("search with no library succeeded, want error")
	}
}

func TestSearchCommand_UnknownLibrary(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "search", "--library", "/local/nope/1", "authentication"); err == nil {
		t.Error("search on an unindexed library succeeded, want error")
	}
}

func TestSearchCommand_RejectsBadLibraryID(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "search", "--library", "not-an-id", "authentication"); err == nil {
		t.Error("search with a malformed library ID succeeded, want error")
	}
}

func TestSearchCommand_NoMatchesIsNotAnError(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "search", "--library", "/local/fixture/1", "quantum chromodynamics")
	if err != nil {
		t.Errorf("search with no matches returned an error: %v\n%s", err, out)
	}
}

func TestSearchCommand_DoesNotLeakRetrievalInternals(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "search", "--library", "/local/fixture/1", "authentication")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	lower := strings.ToLower(out)
	for _, leak := range []string{"distance", "cosine", "topk", "vector", "hnsw"} {
		if strings.Contains(lower, leak) {
			t.Errorf("search output leaks %q:\n%s", leak, out)
		}
	}
}

func TestSearchCommand_FindsContentAcrossPages(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "search", "--library", "/local/fixture/1",
		"how do I exchange client credentials for a bearer token")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if !strings.Contains(out, "auth") {
		t.Errorf("search did not surface the authentication page:\n%s", out)
	}
}
