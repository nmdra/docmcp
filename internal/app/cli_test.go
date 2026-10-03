package app_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nmdra/docmcp/internal/app"
	"github.com/nmdra/docmcp/internal/config"
)

// fixtureSite is a documentation site whose pages can change between runs, which
// is what makes the sync tests meaningful.
type fixtureSite struct {
	*httptest.Server

	mu    sync.Mutex
	pages map[string]string
}

func newFixtureSite(t *testing.T) *fixtureSite {
	t.Helper()

	site := &fixtureSite{pages: map[string]string{
		"/docs/":        docHTML("Docs", "/docs/install", "/docs/api", "/docs/auth"),
		"/docs/install": docHTML("Install"),
		"/docs/api":     docHTML("API"),
		"/docs/auth":    authHTML,
	}}

	var base string
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Write([]byte("User-agent: *\nSitemap: " + base + "/sitemap.xml\n"))
			return
		}
		if r.URL.Path == "/sitemap.xml" {
			w.Write([]byte(`<?xml version="1.0"?><urlset>` +
				`<url><loc>` + base + `/docs/</loc></url>` +
				`<url><loc>` + base + `/docs/install</loc></url>` +
				`<url><loc>` + base + `/docs/api</loc></url>` +
				`<url><loc>` + base + `/docs/auth</loc></url>` +
				`</urlset>`))
			return
		}

		site.mu.Lock()
		body, ok := site.pages[r.URL.Path]
		site.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(body))
	})

	site.Server = httptest.NewServer(mux)
	base = site.URL
	t.Cleanup(site.Close)
	return site
}

func (s *fixtureSite) set(path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages[path] = body
}

func (s *fixtureSite) remove(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, path)
}

func docHTML(title string, links ...string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><title>" + title + " — Fixture</title></head>")
	b.WriteString("<body><nav>nav</nav><main><h1>" + title + "</h1>")
	for _, l := range links {
		b.WriteString(`<a href="` + l + `">` + l + `</a>`)
	}
	b.WriteString(`<p>Body text for ` + title + ` with enough words to be a real chunk.</p>`)
	b.WriteString("</main><footer>footer</footer></body></html>")
	return b.String()
}

const authHTML = `<!doctype html><html><head><title>Auth</title></head><body><main>
<h1>Authentication</h1>
<h2>Token exchange</h2>
<p>Exchange client credentials for a bearer token at the token endpoint.</p>
<pre><code class="language-bash">curl -X POST https://api.fixture.test/oauth/token
</code></pre>
<h2>Key rotation</h2>
<p>Create the new key before revoking the old one to avoid downtime.</p>
</main></body></html>`

// newTestCommand builds the CLI root against a temp data directory with a fake
// embedder, so no test ever reads the user's config or index.
func newTestCommand(t *testing.T) config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.Data.Path = t.TempDir()
	cfg.Embedding.Provider = "fake"

	return cfg
}

func runRoot(t *testing.T, cfg config.Config, args ...string) (string, error) {
	t.Helper()

	return runRootWithConfig(t, cfg, configFileFor(t), args...)
}

// runRootWithConfig runs the CLI against an explicit config file. Indexing and
// serving must use the same one: an index written by a different embedder has
// vectors of a different width and cannot be queried.
func runRootWithConfig(t *testing.T, cfg config.Config, configPath string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	root := app.NewRootCommand("v0.1.0-test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--data-dir", cfg.Data.Path, "--config", configPath}, args...))

	err := root.Execute()
	return out.String(), err
}

func configFileFor(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[embedding]\nprovider = \"fake\"\nmodel = \"fake-model\"\n"
	if err := writeFile(path, body); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestAddCommand_RequiresURL(t *testing.T) {
	cfg := newTestCommand(t)

	_, err := runRoot(t, cfg, "add")
	if err == nil {
		t.Fatal("add with no URL succeeded, want error")
	}
	if !strings.Contains(err.Error(), "arg") && !strings.Contains(err.Error(), "URL") {
		t.Errorf("error = %v, want it to say the URL is required", err)
	}
}

func TestAddCommand_RequiresName(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	_, err := runRoot(t, cfg, "add", site.URL+"/docs/")
	if err == nil {
		t.Fatal("add with no name succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Errorf("error = %v, want it to name the missing --name flag", err)
	}
}

func TestAddCommand_Version(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1.0")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "/local/fixture/1.0") {
		t.Errorf("output missing the library ID:\n%s", out)
	}
}

func TestAddCommand_ReportsPageAndChunkCounts(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1.0")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "discovered:") {
		t.Errorf("output missing the page count:\n%s", out)
	}
	if !strings.Contains(out, "indexed:") {
		t.Errorf("output missing the chunk count:\n%s", out)
	}
}

func TestAddCommand_WithoutVersion(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "/local/fixture") {
		t.Errorf("output missing the versionless library ID:\n%s", out)
	}
}

func TestAddCommand_Includes(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg,
		"add", site.URL+"/docs/", "--name", "fixture", "--include", "/docs/api/**")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	chunks := listChunks(t, cfg)
	for _, c := range chunks {
		if strings.Contains(c.Content, "Body text for Install") {
			t.Error("an excluded page was indexed despite --include")
		}
	}
	if len(chunks) == 0 {
		t.Error("nothing was indexed at all")
	}
}

func TestAddCommand_Excludes(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg,
		"add", site.URL+"/docs/", "--name", "fixture", "--exclude", "/docs/auth")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	for _, c := range listChunks(t, cfg) {
		if strings.Contains(c.Content, "bearer token") {
			t.Error("an excluded page was indexed despite --exclude")
		}
	}
}

func TestAddCommand_Duplicate(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1")
	if err == nil {
		t.Fatal("adding the same library twice succeeded, want error")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error = %v, want it to say the library exists", err)
	}
}

func TestAddCommand_RejectsNonHTTPURL(t *testing.T) {
	cfg := newTestCommand(t)

	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com", "mailto:a@b.c"} {
		if _, err := runRoot(t, cfg, "add", raw, "--name", "x"); err == nil {
			t.Errorf("add %q succeeded, want error", raw)
		}
	}
}

func TestListCommand_Empty(t *testing.T) {
	cfg := newTestCommand(t)

	out, err := runRoot(t, cfg, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("empty list output should say so:\n%s", out)
	}
}

func TestListCommand_MultipleLibraries(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "alpha", "--version", "1"); err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "beta", "--version", "2"); err != nil {
		t.Fatalf("add beta: %v", err)
	}

	out, err := runRoot(t, cfg, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, want := range []string{"/local/alpha/1", "/local/beta/2"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestListCommand_Versions(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	for _, v := range []string{"1", "2"} {
		if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", v); err != nil {
			t.Fatalf("add v%s: %v", v, err)
		}
	}

	out, err := runRoot(t, cfg, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"/local/fixture/1", "/local/fixture/2"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestInfoCommand_ShowsSourceDetail(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runRoot(t, cfg, "info", "/local/fixture/1.0")
	if err != nil {
		t.Fatalf("info: %v\n%s", err, out)
	}

	for _, want := range []string{"fixture", "1.0", site.URL + "/docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("info output missing %q:\n%s", want, out)
		}
	}
}

func TestInfoCommand_UnknownLibrary(t *testing.T) {
	cfg := newTestCommand(t)

	_, err := runRoot(t, cfg, "info", "/local/nope/1")
	if err == nil {
		t.Error("info on an unknown library succeeded, want error")
	}
}

func TestRemoveCommand(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(listChunks(t, cfg)) == 0 {
		t.Fatal("nothing was indexed")
	}

	out, err := runRoot(t, cfg, "remove", "fixture")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}

	if got := listChunks(t, cfg); len(got) != 0 {
		t.Errorf("%d chunks survived a remove", len(got))
	}

	listOut, err := runRoot(t, cfg, "list")
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if strings.Contains(listOut, "/local/fixture") {
		t.Errorf("removed library still listed:\n%s", listOut)
	}
}

func TestRemoveCommand_UnknownSource(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "remove", "nope"); err == nil {
		t.Error("remove of an unknown source succeeded, want error")
	}
}

func TestSyncCommand_SkipsUnchangedWork(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	firstCount := len(listChunks(t, cfg))

	out, err := runRoot(t, cfg, "sync", "fixture")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	// The whole point of sync: an unchanged site adds nothing.
	if !strings.Contains(out, "unchanged:  4") {
		t.Errorf("sync output missing the unchanged count:\n%s", out)
	}
	if !strings.Contains(out, "added:      0") {
		t.Errorf("sync of an unchanged site added chunks:\n%s", out)
	}
	if got := len(listChunks(t, cfg)); got != firstCount {
		t.Errorf("chunk count changed on a no-op sync: %d -> %d", firstCount, got)
	}
}

func TestSyncCommand_PicksUpChangedPage(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	site.set("/docs/api", docHTML("API", "Rewritten body text about websocket upgrades."))

	out, err := runRoot(t, cfg, "sync", "fixture")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated") && !strings.Contains(out, "added") {
		t.Errorf("sync output shows no work after a page changed:\n%s", out)
	}

	found := false
	for _, c := range listChunks(t, cfg) {
		if strings.Contains(c.Content, "websocket upgrades") {
			found = true
		}
	}
	if !found {
		t.Error("the changed page's new content was not indexed")
	}
}

func TestSyncCommand_RemovesDeletedPage(t *testing.T) {
	site := newFixtureSite(t)
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "add", site.URL+"/docs/", "--name", "fixture", "--version", "1"); err != nil {
		t.Fatalf("add: %v", err)
	}

	before := len(listChunks(t, cfg))
	site.remove("/docs/api")

	out, err := runRoot(t, cfg, "sync", "fixture")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	if after := len(listChunks(t, cfg)); after >= before {
		t.Errorf("sync left %d chunks after a page was removed, want fewer than %d", after, before)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("sync output missing the removed count:\n%s", out)
	}
}

func TestSyncCommand_UnknownSource(t *testing.T) {
	cfg := newTestCommand(t)

	if _, err := runRoot(t, cfg, "sync", "nope"); err == nil {
		t.Error("sync of an unknown source succeeded, want error")
	}
}
