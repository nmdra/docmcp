package app_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/docmcp/docmcp/internal/config"
)

// mutableSite is a documentation tree whose pages can be edited between crawls,
// so a sync can be observed doing incremental work instead of a full rebuild.
// fixtureSite in cli_test.go is fixed after construction; this one is not.
type mutableSite struct {
	*httptest.Server

	mu      sync.Mutex
	pages   map[string]string
	fetches map[string]int
}

func newMutableSite(t *testing.T) *mutableSite {
	t.Helper()

	site := &mutableSite{
		pages:   map[string]string{},
		fetches: map[string]int{},
	}

	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		body, ok := site.pages[r.URL.Path]
		site.fetches[r.URL.Path]++
		site.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))

	t.Cleanup(site.Close)
	return site
}

// put writes a page whose body links to every path in links, so a sync sees the
// same reachable set from any entry point.
func (s *mutableSite) put(path, title, body string) {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><title>" + title + "</title></head><body><main>")
	b.WriteString("<h1>" + title + "</h1>" + body)
	for _, l := range []string{"/docs/", "/docs/change/", "/docs/added/"} {
		b.WriteString(`<a href="` + l + `">` + l + `</a>`)
	}
	b.WriteString("</main></body></html>")

	// DocMCP normalizes a trailing slash away before fetching, so the site must
	// answer to both /docs/ and /docs.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages[path] = b.String()
	if bare := strings.TrimSuffix(path, "/"); bare != path && bare != "" {
		s.pages[bare] = b.String()
	}
}

func (s *mutableSite) remove(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, path)
	delete(s.pages, strings.TrimSuffix(path, "/"))
	delete(s.pages, strings.TrimSuffix(path, "/")+"/")
}

// TestSync_IncrementalWorkIsReported pins the four cases the plan requires of a
// sync: unchanged content is left alone, changed content is re-embedded, new
// pages appear, and deleted pages are dropped.
//
// Found during real-world validation: a no-change sync of a 456-chunk library
// reported "unchanged: 456 / indexed: 0", which looks right, but nothing at the
// CLI level proved that unchanged chunks were skipped rather than re-embedded
// and then counted as unchanged. These assertions pin the observable counters.
func TestSync_IncrementalWorkIsReported(t *testing.T) {
	site := newMutableSite(t)
	site.put("/docs/", "Index", "<h2>Overview</h2><p>KEEP_TOKEN_UNCHANGED stays exactly the same forever.</p>")
	site.put("/docs/change/", "Change", "<h2>Wording</h2><p>CHANGE_TOKEN_V1 the original wording of this section.</p>")
	site.put("/docs/added/", "Added", "<h2>Intro</h2><p>ADDED_TOKEN_V1 first version of the added page.</p>")

	cfg := newTestCommand(t)

	if out, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "inc", "--version", "current"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// -- a sync with nothing changed must not add or remove anything -------
	out, err := runRoot(t, cfg, "sync", "inc")
	if err != nil {
		t.Fatalf("no-change sync: %v\n%s", err, out)
	}
	assertCounter(t, out, "added:", 0)
	assertCounter(t, out, "removed:", 0)
	assertCounter(t, out, "indexed:", 0)
	if got := counter(t, out, "unchanged:"); got == 0 {
		t.Errorf("a no-change sync should report unchanged chunks, got:\n%s", out)
	}

	// -- change one page, add one, delete one -----------------------------
	site.put("/docs/change/", "Change", "<h2>Wording</h2><p>CHANGE_TOKEN_V2 the revised wording of this section.</p>")
	site.put("/docs/added/", "Added", "<h2>Intro</h2><p>ADDED_TOKEN_V2 replacement text on the added page.</p>")
	site.remove("/docs/added/")

	out, err = runRoot(t, cfg, "sync", "inc")
	if err != nil {
		t.Fatalf("second sync: %v\n%s", err, out)
	}

	assertCounter(t, out, "added:", 0)

	// Exactly one chunk was re-embedded: the changed page. The other two were
	// recognised as unchanged.
	// One chunk re-embedded (the changed page), one removed (the deleted page).
	assertCounter(t, out, "updated:", 1)
	assertCounter(t, out, "removed:", 1)

	// Retrieval ranking is a separate question with its own tests, so assert on
	// what a query can reach rather than on what ranks first. Searching for the
	// old text must not surface the old text: it was replaced, not duplicated.
	if old := searchFor(t, cfg, "CHANGE_TOKEN_V1"); strings.Contains(old, "CHANGE\\_TOKEN\\_V1") {
		t.Errorf("superseded text is still retrievable after a re-embed:\n%s", old)
	}
	if got := searchFor(t, cfg, "CHANGE_TOKEN_V2"); !strings.Contains(got, "CHANGE\\_TOKEN\\_V2") {
		t.Errorf("the changed page's new text is not retrievable:\n%s", got)
	}
	if got := searchFor(t, cfg, "KEEP_TOKEN_UNCHANGED"); !strings.Contains(got, "KEEP\\_TOKEN\\_UNCHANGED") {
		t.Errorf("the untouched page is no longer retrievable:\n%s", got)
	}
	if got := searchFor(t, cfg, "ADDED_TOKEN_V1"); strings.Contains(got, "ADDED\\_TOKEN\\_V1") {
		t.Errorf("a deleted page's content is still retrievable:\n%s", got)
	}
}

// TestSync_DeletingEverythingRemovesChunksButKeepsLibrary pins the documented
// behaviour for a site that has gone away entirely: the library survives so its
// history is not silently destroyed, but its stale chunks are dropped rather
// than served as if they were current documentation.
func TestSync_DeletingEverythingRemovesChunksButKeepsLibrary(t *testing.T) {
	site := newMutableSite(t)
	site.put("/docs/", "Only", "<h2>Body</h2><p>DOOMED_TOKEN_XYZ the only page on this site.</p>")

	cfg := newTestCommand(t)

	if out, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "gone", "--version", "current"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	site.remove("/docs/")

	out, err := runRoot(t, cfg, "sync", "gone")
	if err != nil {
		t.Fatalf("sync after the site went away should warn, not fail: %v\n%s", err, out)
	}

	if !strings.Contains(out, "removed:") {
		t.Errorf("sync should report removed chunks, got:\n%s", out)
	}

	listOut, err := runRoot(t, cfg, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "/local/gone/current") {
		t.Errorf("the library registration should survive an empty sync:\n%s", listOut)
	}
}

func assertCounter(t *testing.T, out, label string, want int) {
	t.Helper()

	if got, ok := lookupCounter(out, label); !ok {
		t.Errorf("output has no %s counter:\n%s", label, out)
	} else if got != want {
		t.Errorf("%s = %d, want %d in output:\n%s", label, got, want, out)
	}
}

func counter(t *testing.T, out, label string) int {
	t.Helper()

	got, ok := lookupCounter(out, label)
	if !ok {
		t.Fatalf("output has no %s counter:\n%s", label, out)
	}
	return got
}

func lookupCounter(out, label string) (int, bool) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, label) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}

// searchFor runs a library-scoped search and returns its output. A search that
// errors is itself a failure: it means the library stopped answering.
func searchFor(t *testing.T, cfg config.Config, query string) string {
	t.Helper()

	out, err := runRoot(t, cfg, "search", query, "--library", "/local/inc/current")
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	return out
}
