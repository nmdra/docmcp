package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nmdra/docmcp/internal/config"
)

// mcpDocsSite is the fixture from the plan's golden scenario: a two-section page
// about MCP tool exposure, where one section is the answer and the other is a
// plausible decoy.
func newMCPDocsSite(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	var base string
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\nSitemap: " + base + "/sitemap.xml\n"))
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><urlset>` +
			`<url><loc>` + base + `/docs/mcp</loc></url>` +
			`</urlset>`))
	})
	mux.HandleFunc("/docs/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>MCP</title></head><body>
<nav>navigation</nav>
<main>
  <h1>MCP</h1>

  <h2>Tool Exposure</h2>
  <p>Tools using codemode exposure are discovered lazily. The server returns
  a tool manifest on first call and loads implementations on demand, which keeps
  startup fast when a session touches only a few tools.</p>

  <h2>Direct Exposure</h2>
  <p>Direct tools are immediately available. Every tool in the manifest is
  loaded during initialization, which costs startup time proportional to the
  total number of registered tools.</p>
</main>
<footer>footer</footer>
</body></html>`))
	})

	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// TestEndToEnd_ResolvesAndAnswers proves the whole system: a documentation site
// is indexed, then a question is asked of it through the MCP surface and comes
// back with the right section.
func TestEndToEnd_ResolvesAndAnswers(t *testing.T) {
	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	proc := startServe(t, cfg.Data.Path)
	proc.initialize(t)

	resolved := proc.call(t, 2, "resolve-library-id", map[string]any{
		"libraryName": "Acme",
		"query":       "how are codemode tools discovered",
	})
	if result, _ := resolved["result"].(map[string]any); result["isError"] == true {
		t.Fatalf("resolve failed: %s", textOf(t, resolved))
	}
	if got := textOf(t, resolved); !strings.Contains(got, "/local/acme/1.0") {
		t.Fatalf("resolve did not return the indexed library:\n%s", got)
	}

	answered := proc.call(t, 3, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	})
	if result, _ := answered["result"].(map[string]any); result["isError"] == true {
		t.Fatalf("query failed: %s", textOf(t, answered))
	}

	content := textOf(t, answered)

	if !strings.Contains(content, "Tool Exposure") {
		t.Errorf("answer is missing the Tool Exposure section:\n%s", content)
	}
	if !strings.Contains(content, "discovered lazily") {
		t.Errorf("answer is missing the lazily-discovered detail:\n%s", content)
	}
}

// TestEndToEnd_PrefersRelevantSectionOverDecoy is the retrieval-quality
// regression guard from the plan: the right section must win over a section that
// is equally on-topic but answers a different question.
func TestEndToEnd_PrefersRelevantSectionOverDecoy(t *testing.T) {
	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	proc := startServe(t, cfg.Data.Path)
	proc.initialize(t)

	answered := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	})
	content := textOf(t, answered)

	lazy := strings.Index(content, "discovered lazily")
	if lazy < 0 {
		t.Fatalf("answer missing the lazy-discovery section:\n%s", content)
	}

	// The decoy section is legitimately about tools, so it may appear. What must
	// not happen is the decoy outranking the correct answer.
	direct := strings.Index(content, "immediately available")

	switch {
	case direct < 0:
		// Ideal: only the relevant section came back.
	case direct < lazy:
		t.Errorf("the decoy section outranked the correct answer:\n%s", content)
	}
}

// TestEndToEnd_VersionIsolation proves two versions of one library never bleed
// into each other's answers.
func TestEndToEnd_VersionIsolation(t *testing.T) {
	var v1, v2 *httptest.Server

	for _, spec := range []struct {
		body    string
		assign  **httptest.Server
		pathTag string
	}{
		{
			body:    "<h2>Authentication</h2><p>Authentication uses API keys sent in the X-Acme-Key header.</p>",
			assign:  &v1,
			pathTag: "v1",
		},
		{
			body:    "<h2>Authentication</h2><p>Authentication uses OAuth bearer tokens obtained from the token endpoint.</p>",
			assign:  &v2,
			pathTag: "v2",
		},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/robots.txt":
				w.Write([]byte("User-agent: *\n"))
			default:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte("<!doctype html><html><head><title>Docs</title></head><body><main>" +
					"<h1>Docs</h1>" + spec.body + "</main></body></html>"))
			}
		}))
		t.Cleanup(server.Close)
		*spec.assign = server
	}

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", v1.URL+"/docs/", "--name", "acme", "--version", "1"); err != nil {
		t.Fatalf("add v1: %v", err)
	}
	if _, err := runRoot(t, cfg, "add", v2.URL+"/docs/", "--name", "acme", "--version", "2"); err != nil {
		t.Fatalf("add v2: %v", err)
	}

	proc := startServe(t, cfg.Data.Path)
	proc.initialize(t)

	answer := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/acme/1",
		"query":     "how does authentication work",
	})
	content := textOf(t, answer)

	if strings.Contains(content, "OAuth") {
		t.Errorf("version 2 content leaked into a version 1 query:\n%s", content)
	}
	if !strings.Contains(content, "API keys") {
		t.Errorf("version 1 query did not return version 1 content:\n%s", content)
	}
}

// TestEndToEnd_SyncKeepsAnswersStable proves a re-sync does not degrade what is
// already indexed.
func TestEndToEnd_SyncKeepsAnswersStable(t *testing.T) {
	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	first := startServe(t, cfg.Data.Path)
	first.initialize(t)
	before := textOf(t, first.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	}))

	if _, err := runRoot(t, cfg, "sync", "acme"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	second := startServe(t, cfg.Data.Path)
	second.initialize(t)
	after := textOf(t, second.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	}))

	if before != after {
		t.Errorf("the answer changed across a no-op sync:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestEndToEnd_RemoveClearsAnswers proves a removed library stops answering,
// rather than serving orphaned chunks.
func TestEndToEnd_RemoveClearsAnswers(t *testing.T) {
	site := newMCPDocsSite(t)

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/mcp",
		"--name", "acme", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := runRoot(t, cfg, "remove", "acme"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	proc := startServe(t, cfg.Data.Path)
	proc.initialize(t)

	answered := proc.call(t, 2, "query-docs", map[string]any{
		"libraryId": "/local/acme/1.0",
		"query":     "How are codemode tools discovered?",
	})

	result, _ := answered["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("a removed library still answered:\n%s", textOf(t, answered))
	}
	if strings.Contains(textOf(t, answered), "discovered lazily") {
		t.Error("removed content is still being served")
	}
}
