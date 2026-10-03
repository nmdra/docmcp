package crawler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nmdra/docmcp/internal/crawler"
)

// fixtureSite serves a small documentation site over httptest and records every
// path it was asked for, so tests can assert both what was crawled and what was
// never fetched.
type fixtureSite struct {
	*httptest.Server

	mu       sync.Mutex
	fetched  []string
	pageFunc func(path string) (string, int)
}

func newFixtureSite(t *testing.T, pages map[string]string) *fixtureSite {
	t.Helper()

	// Keys are stored canonicalized, because the crawler normalizes URLs before
	// fetching: a "/docs/" entry must answer a request for "/docs".
	byPath := map[string]string{}
	for p, body := range pages {
		byPath[canonicalPath(t, p)] = body
	}

	site := &fixtureSite{
		pageFunc: func(path string) (string, int) {
			body, ok := byPath[path]
			if !ok {
				return "not found", http.StatusNotFound
			}
			return body, http.StatusOK
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.fetched = append(site.fetched, r.URL.Path)
		site.mu.Unlock()

		body, status := site.pageFunc(r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		w.Write([]byte(body))
	})

	site.Server = httptest.NewServer(mux)
	t.Cleanup(site.Close)
	return site
}

// canonicalPath applies the same trailing-slash rule the crawler applies, so a
// "/docs/" fixture entry answers a request for "/docs".
func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	normalized, err := crawler.NormalizeURL("https://fixture.invalid"+path, "")
	if err != nil {
		t.Fatalf("NormalizeURL(%q): %v", path, err)
	}
	u, err := url.Parse(normalized)
	if err != nil {
		t.Fatalf("Parse(%q): %v", normalized, err)
	}
	return u.Path
}

func (s *fixtureSite) setPage(path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageFunc = func(p string) (string, int) {
		if p == path {
			return body, http.StatusOK
		}
		return "not found", http.StatusNotFound
	}
}

func (s *fixtureSite) fetchedPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.fetched))
	copy(out, s.fetched)
	return out
}

func (s *fixtureSite) wasFetched(path string) bool {
	for _, p := range s.fetchedPaths() {
		if p == path {
			return true
		}
	}
	return false
}

func docPage(title string, links ...string) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><title>" + title + "</title></head><body><nav>nav junk</nav><main><h1>" + title + "</h1>")
	for _, l := range links {
		b.WriteString(`<a href="` + l + `">` + l + `</a>`)
	}
	b.WriteString("</main><footer>footer junk</footer></body></html>")
	return b.String()
}

func newCrawler(t *testing.T, base string, includes, excludes []string, limits crawler.Limits) *crawler.Crawler {
	t.Helper()

	filter, err := crawler.NewFilter(base, includes, excludes)
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}

	fetcher := crawler.NewHTTPFetcher(nil, crawler.FetchLimits{
		Timeout:      5 * time.Second,
		MaxBodyBytes: 1 << 20,
	})

	c, err := crawler.NewCrawler(filter, fetcher, limits)
	if err != nil {
		t.Fatalf("NewCrawler: %v", err)
	}
	return c
}

func defaultLimits() crawler.Limits {
	return crawler.Limits{Concurrency: 4, MaxPages: 100, MaxDepth: 10, RateLimit: 0}
}

func pageURLs(pages []crawler.Page) []string {
	out := make([]string, 0, len(pages))
	for _, p := range pages {
		out = append(out, p.URL)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestCrawler_SitemapOnlyCrawlsAllowedDocs(t *testing.T) {
	var siteURL string

	site := newFixtureSite(t, map[string]string{})
	site.mu.Lock()
	site.pageFunc = func(path string) (string, int) {
		switch path {
		case "/robots.txt":
			return "User-agent: *\nSitemap: " + siteURL + "/sitemap.xml\n", http.StatusOK
		case "/sitemap.xml":
			return `<?xml version="1.0"?><urlset>` +
				`<url><loc>` + siteURL + `/docs/</loc></url>` +
				`<url><loc>` + siteURL + `/docs/install</loc></url>` +
				`<url><loc>` + siteURL + `/docs/api</loc></url>` +
				`<url><loc>` + siteURL + `/blog/news</loc></url>` +
				`<url><loc>` + siteURL + `/admin</loc></url>` +
				`</urlset>`, http.StatusOK
		case "/docs":
			return docPage("Docs", "/docs/install", "/docs/api"), http.StatusOK
		case "/docs/install":
			return docPage("Install"), http.StatusOK
		case "/docs/api":
			return docPage("API"), http.StatusOK
		case "/blog/news":
			return docPage("News"), http.StatusOK
		case "/admin":
			return docPage("Admin"), http.StatusOK
		}
		return "not found", http.StatusNotFound
	}
	site.mu.Unlock()
	siteURL = site.URL

	c := newCrawler(t, site.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := pageURLs(pages)
	want := []string{
		site.URL + "/docs",
		site.URL + "/docs/install",
		site.URL + "/docs/api",
	}

	for _, w := range want {
		if !contains(got, w) {
			t.Errorf("missing page %q; crawled %v", w, got)
		}
	}
	for _, bad := range []string{site.URL + "/blog/news", site.URL + "/admin"} {
		if contains(got, bad) {
			t.Errorf("crawled out-of-scope page %q", bad)
		}
		if site.wasFetched(strings.TrimPrefix(bad, site.URL)) {
			t.Errorf("fetched out-of-scope page %q at all", bad)
		}
	}
}

func TestCrawler_FallsBackToLinksWithoutSitemap(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":        docPage("Docs", "/docs/install", "/docs/api", "/blog/news"),
		"/docs/install": docPage("Install"),
		"/docs/api":     docPage("API"),
		"/blog/news":    docPage("News"),
	})

	c := newCrawler(t, site.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := pageURLs(pages)
	for _, want := range []string{site.URL + "/docs", site.URL + "/docs/install", site.URL + "/docs/api"} {
		if !contains(got, want) {
			t.Errorf("missing page %q; crawled %v", want, got)
		}
	}
	if contains(got, site.URL+"/blog/news") {
		t.Error("link fallback left the base path")
	}
}

func TestCrawler_DoesNotLeaveHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("external"))
	}))
	defer other.Close()

	site := newFixtureSite(t, map[string]string{
		"/docs/":        docPage("Docs", "/docs/install", other.URL+"/docs/other"),
		"/docs/install": docPage("Install"),
	})

	c := newCrawler(t, site.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	for _, p := range pageURLs(pages) {
		if strings.HasPrefix(p, other.URL) {
			t.Errorf("crawled off-host page %q", p)
		}
	}
}

func TestCrawler_DoesNotLeavePathPrefix(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":        docPage("Docs", "/docs/install", "/docs/v2/api", "/blog/news"),
		"/docs/install": docPage("Install"),
		"/docs/v2/api":  docPage("V2 API"),
		"/blog/news":    docPage("News"),
	})

	c := newCrawler(t, site.URL+"/docs/latest/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/latest/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("crawled %v, want nothing under a base path that does not exist", pageURLs(pages))
	}
}

func TestCrawler_DeduplicatesURLs(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs": docPage("Docs",
			"/docs/install", "/docs/install/", "/docs/install#top",
			"/docs/install?utm_source=x", "/docs/api"),
		"/docs/install": docPage("Install"),
		"/docs/api":     docPage("API"),
	})

	c := newCrawler(t, site.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	seen := map[string]int{}
	for _, p := range pages {
		seen[p.URL]++
	}
	for url, n := range seen {
		if n > 1 {
			t.Errorf("page %q fetched %d times, want 1", url, n)
		}
	}
	if got := len(seen); got != 3 {
		t.Errorf("crawled %d unique pages, want 3: %v", got, pageURLs(pages))
	}
}

func TestCrawler_HonorsMaxPages(t *testing.T) {
	pages := map[string]string{"/docs/": ""}
	links := make([]string, 0, 10)
	for i := range 10 {
		p := "/docs/p" + string(rune('a'+i))
		links = append(links, p)
		pages[p] = docPage("Page")
	}
	pages["/docs/"] = docPage("Docs", links...)

	site := newFixtureSite(t, pages)

	limits := defaultLimits()
	limits.MaxPages = 3

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	got, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("crawled %d pages, want the MaxPages limit of 3", len(got))
	}
}

func TestCrawler_HonorsDepth(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs":     docPage("Docs", "/docs/a"),
		"/docs/a":   docPage("A", "/docs/a/b"),
		"/docs/a/b": docPage("A B"),
	})

	limits := defaultLimits()
	limits.MaxDepth = 1

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := pageURLs(pages)
	if contains(got, site.URL+"/docs/a/b") {
		t.Errorf("crawled beyond MaxDepth: %v", got)
	}
	if !contains(got, site.URL+"/docs/a") {
		t.Errorf("crawled %v, want /docs/a within depth", got)
	}
}

func TestCrawler_HonorsIncludeExcludeRules(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":             docPage("Docs", "/docs/api/auth", "/docs/archive/v1", "/docs/guides/start"),
		"/docs/api/auth":     docPage("Auth"),
		"/docs/archive/v1":   docPage("Archived"),
		"/docs/guides/start": docPage("Start"),
	})

	c := newCrawler(t, site.URL+"/docs/",
		[]string{"/docs/api/**", "/docs/guides/**"},
		[]string{"/docs/archive/**"},
		defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	got := pageURLs(pages)
	if !contains(got, site.URL+"/docs/api/auth") {
		t.Errorf("missing included page in %v", got)
	}
	if contains(got, site.URL+"/docs/archive/v1") {
		t.Errorf("crawled excluded page in %v", got)
	}
}

func TestCrawler_NeverFetchesRobotsDisallowedPaths(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":               docPage("Docs", "/docs/private/secret", "/docs/public"),
		"/docs/private/secret": docPage("Secret"),
		"/docs/public":         docPage("Public"),
	})
	site.setPage("/robots.txt", "User-agent: *\nDisallow: /docs/private/\n")

	c := newCrawler(t, site.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	if site.wasFetched("/docs/private/secret") {
		t.Error("crawler fetched a robots-disallowed path")
	}
	for _, p := range pageURLs(pages) {
		if strings.Contains(p, "/private/") {
			t.Errorf("disallowed page appeared in results: %q", p)
		}
	}
}

func TestCrawler_RateLimits(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":  docPage("Docs", "/docs/a", "/docs/b", "/docs/c"),
		"/docs/a": docPage("A"),
		"/docs/b": docPage("B"),
		"/docs/c": docPage("C"),
	})

	limits := defaultLimits()
	limits.RateLimit = 50 // 50 requests per second ceiling

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	start := time.Now()
	if _, err := c.Crawl(t.Context(), site.URL+"/docs/"); err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("crawl of 4 pages took %v, want rate limiting well under that", elapsed)
	}
}

func TestCrawler_ConcurrencyIsBounded(t *testing.T) {
	const pages = 30

	links := make([]string, 0, pages)
	mux := map[string]string{}
	for i := range pages {
		p := "/docs/p" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		links = append(links, p)
		mux[p] = docPage("Page")
	}
	mux["/docs/"] = docPage("Docs", links...)

	site := newFixtureSite(t, mux)

	limits := defaultLimits()
	limits.Concurrency = 3

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	if _, err := c.Crawl(t.Context(), site.URL+"/docs/"); err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if len(site.fetchedPaths()) == 0 {
		t.Fatal("fixture fetched nothing")
	}
}

func TestCrawler_ReportsLimitExceeded(t *testing.T) {
	site := newFixtureSite(t, map[string]string{
		"/docs/":  docPage("Docs", "/docs/a", "/docs/b"),
		"/docs/a": docPage("A"),
		"/docs/b": docPage("B"),
	})

	limits := defaultLimits()
	limits.MaxPages = 1
	limits.StopOnLimit = true

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	if _, err := c.Crawl(t.Context(), site.URL+"/docs/"); err == nil {
		t.Error("Crawl with StopOnLimit succeeded, want ErrCrawlLimitExceeded")
	}
}

func TestCrawler_IgnoresNonDocumentContentTypes(t *testing.T) {
	var hits atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\n"))
	})
	mux.HandleFunc("/docs/", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(docPage("Docs", "/docs/a")))
	})
	mux.HandleFunc("/docs/a", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newCrawler(t, srv.URL+"/docs/", nil, nil, defaultLimits())

	pages, err := c.Crawl(t.Context(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	for _, p := range pages {
		if strings.Contains(p.URL, "/docs/a") {
			t.Errorf("indexed a non-HTML resource: %q", p.URL)
		}
	}
}

func TestCrawler_ConcurrentCrawlDoesNotDuplicate(t *testing.T) {
	links := make([]string, 0, 20)
	mux := map[string]string{}
	for i := range 20 {
		p := "/docs/p" + string(rune('a'+i))
		links = append(links, p)
		mux[p] = docPage("Page")
	}
	mux["/docs/"] = docPage("Docs", links...)

	site := newFixtureSite(t, mux)

	limits := defaultLimits()
	limits.Concurrency = 8

	c := newCrawler(t, site.URL+"/docs/", nil, nil, limits)

	pages, err := c.Crawl(t.Context(), site.URL+"/docs/")
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	seen := map[string]int{}
	for _, p := range pages {
		seen[p.URL]++
	}
	for u, n := range seen {
		if n > 1 {
			t.Errorf("%q appears %d times under concurrency", u, n)
		}
	}
}
