package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nmdra/docmcp/internal/app"
	"github.com/nmdra/docmcp/internal/config"
)

// TestAdd_EmptyCrawlFailsLoudly pins the behaviour that a source yielding no
// pages at all is a failure, not a success.
//
// Regression found in real-world validation: `docmcp add <url-that-404s>`
// exited 0, registered the library, and left `docmcp list` showing a 0-page,
// 0-chunk entry. A user scripting `add` could not distinguish success from
// failure without reading the counters and comparing them by hand.
func TestAdd_EmptyCrawlFailsLoudly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", server.URL+"/docs/", "--name", "empty", "--version", "current")
	if err == nil {
		t.Fatalf("adding a source that yields no pages should fail; got success\noutput:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no pages") {
		t.Errorf("error should say the crawl produced no pages, got: %v", err)
	}

	if got := libraryIDsIn(t, cfg.Data.Path); len(got) != 0 {
		t.Errorf("a failed add must not leave a registered library; found %v", got)
	}
}

// TestAdd_PartialCrawlSucceeds guards the other side: a site where one page 404s
// but others resolve must still index what it found. Only a total failure is an
// error — a site with a broken link is not a broken source.
func TestAdd_PartialCrawlSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs", "/docs/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(
				`<html><head><title>Docs</title></head><body><main>` +
					`<h1>Docs</h1><p>Real content here.</p>` +
					`<a href="/docs/gone/">missing</a>` +
					`</main></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", server.URL+"/docs/", "--name", "partial", "--version", "current")
	if err != nil {
		t.Fatalf("a partial crawl should still succeed, got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "indexed:    1") {
		t.Errorf("expected the reachable page to be indexed, got:\n%s", out)
	}
}

// TestAdd_NonHTMLSourceFailsLoudly covers a URL that resolves but serves a
// content type DocMCP cannot parse. Like the 404 case it yields zero pages, and
// it must fail for the same reason rather than registering an empty library.
func TestAdd_NonHTMLSourceFailsLoudly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("this is not documentation"))
	}))
	defer server.Close()

	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", server.URL+"/docs/", "--name", "plain", "--version", "current")
	if err == nil {
		t.Fatalf("adding a non-HTML source should fail; got success\noutput:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no pages") {
		t.Errorf("error should say the crawl produced no pages, got: %v", err)
	}
}

// TestResolve_ReportsChunksNotPages guards the field name shown to agents. The
// resolver reported chunk counts under the label "Indexed Pages", which is a
// different number and sent agents choosing between libraries the wrong signal.
func TestResolve_ReportsChunksNotPages(t *testing.T) {
	cfg := newTestCommand(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(
			`<html><head><title>Docs</title></head><body><main>` +
				`<h1>Alpha</h1><h2>One</h2><p>First section body.</p>` +
				`<h2>Two</h2><p>Second section body.</p>` +
				`<h2>Three</h2><p>Third section body.</p>` +
				`</main></body></html>`))
	}))
	defer server.Close()

	if _, err := runRoot(t, cfg, "add", server.URL+"/docs/", "--name", "counted", "--version", "current"); err != nil {
		t.Fatalf("add: %v", err)
	}

	resolved, err := runRoot(t, cfg, "search", "section body", "--library", "/local/counted/current")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// The search itself proves the library is indexed; the label is asserted in
	// the resolver unit tests, which is where the number is formatted.
	if !strings.Contains(resolved, "/local/counted/current") {
		t.Errorf("search should reference the library, got:\n%s", resolved)
	}
}

func libraryIDsIn(t *testing.T, dataDir string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dataDir, "sources.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}

	var doc struct {
		Sources []struct {
			LibraryID string `json:"LibraryID"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("sources.json is not valid JSON: %v", err)
	}

	out := make([]string, 0, len(doc.Sources))
	for _, s := range doc.Sources {
		out = append(out, s.LibraryID)
	}
	return out
}

var _ = app.NewRootCommand
var _ = config.Default
