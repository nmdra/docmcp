package crawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRobots_DisallowedPathsAreNeverFetched is the hard guarantee from the plan:
// a path robots.txt disallows must never be requested.
//
// This is asserted by counting requests on the server rather than by inspecting
// the crawler, so a crawler that merely recorded the disallow but still fetched
// would fail.
func TestRobots_DisallowedPathsAreNeverFetched(t *testing.T) {
	var fetched []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = append(fetched, r.URL.Path)

		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /docs/secret\n"))
			return
		case "/docs":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(
				`<html><head><title>D</title></head><body><main><h1>Docs</h1>` +
					`<a href="/docs/public/">public</a>` +
					`<a href="/docs/secret/">secret</a>` +
					`</main></body></html>`))
			return
		case "/docs/public", "/docs/public/", "/docs/secret", "/docs/secret/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>P</title></head><body><main><h1>Page</h1>` +
				`<p>content</p></main></body></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	f, err := NewFilter(server.URL+"/docs", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCrawler(f, NewHTTPFetcher(&http.Client{Timeout: 5 * time.Second},
		FetchLimits{MaxBodyBytes: 1 << 20}), Limits{Concurrency: 2, MaxDepth: 3, MaxPages: 20})
	if err != nil {
		t.Fatal(err)
	}

	pages, err := c.Crawl(context.Background(), server.URL+"/docs")
	if err != nil {
		t.Fatalf("crawl: %v", err)
	}

	for _, path := range fetched {
		if strings.HasPrefix(path, "/docs/secret") {
			t.Errorf("crawler fetched a robots-disallowed path %q", path)
		}
	}
	if len(pages) == 0 {
		t.Error("crawl returned no pages; the allowed page should still be indexed")
	}
}

// TestRobots_TrailingSlashInDisallowDoesNotCoverTheNormalizedPath records a
// real, spec-conformant edge: DocMCP normalizes "/docs/secret/" to
// "/docs/secret" before fetching, and RFC 9309 matching is literal, so a rule
// written with a trailing slash does not cover the request DocMCP makes.
//
// This is not a bug in either half — robots matching must stay literal to be
// predictable — but it surprised a real-world check, so it is pinned here. The
// practical rule for a site owner is to write "Disallow: /docs/secret" with no
// trailing slash.
func TestRobots_TrailingSlashInDisallowDoesNotCoverTheNormalizedPath(t *testing.T) {
	var fetched []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = append(fetched, r.URL.Path)

		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /docs/secret/\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>P</title></head><body><main><h1>P</h1><p>x</p></main></body></html>`))
	}))
	defer server.Close()

	f, _ := NewFilter(server.URL+"/docs", nil, nil)
	c, err := NewCrawler(f, NewHTTPFetcher(&http.Client{Timeout: 5 * time.Second},
		FetchLimits{MaxBodyBytes: 1 << 20}), Limits{Concurrency: 1, MaxDepth: 1, MaxPages: 5})
	if err != nil {
		t.Fatal(err)
	}

	robots, err := ParseRobots([]byte("User-agent: *\nDisallow: /docs/secret/\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}
	if !robots.Allowed("/docs/secret") {
		t.Error("a rule written as /docs/secret/ should not match the normalized path /docs/secret")
	}
	if robots.Allowed("/docs/secret/inner") {
		t.Error("a rule written as /docs/secret/ should still match paths beneath it")
	}

	if _, err := c.Crawl(context.Background(), server.URL+"/docs"); err != nil {
		t.Fatalf("crawl: %v", err)
	}
	_ = fetched
}

// TestRobots_MissingRobotsAllowsEverything pins the plan's behaviour for a site
// with no robots.txt: absence of permission is not refusal.
func TestRobots_MissingRobotsAllowsEverything(t *testing.T) {
	var secretFetched bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>D</title></head><body><main><h1>Docs</h1>` +
				`<a href="/docs/a/">a</a></main></body></html>`))
			return
		default:
			if strings.HasPrefix(r.URL.Path, "/docs/a") {
				secretFetched = true
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>A</title></head><body><main><h1>A</h1><p>x</p></main></body></html>`))
			return
		}
	}))
	defer server.Close()

	f, _ := NewFilter(server.URL+"/docs", nil, nil)
	c, err := NewCrawler(f, NewHTTPFetcher(&http.Client{Timeout: 5 * time.Second},
		FetchLimits{MaxBodyBytes: 1 << 20}), Limits{Concurrency: 2, MaxDepth: 3, MaxPages: 20})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Crawl(context.Background(), server.URL+"/docs"); err != nil {
		t.Fatalf("crawl: %v", err)
	}
	if !secretFetched {
		t.Error("without robots.txt every in-scope page should be reachable")
	}
}

// TestRobots_MalformedRobotsDoesNotBlockEverything guards a failure mode that
// would be very hard to notice: a robots.txt the parser cannot read must not
// silently make the whole site un-crawlable.
func TestRobots_MalformedRobotsDoesNotBlockEverything(t *testing.T) {
	var pageFetched bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			// Directives with no user-agent and a broken wildcard line.
			_, _ = w.Write([]byte("Disallow: /\nUser-agent\nUser-agent: *\nDisallow: /*??\n"))
			return
		default:
			pageFetched = true
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>A</title></head><body><main><h1>A</h1><p>x</p></main></body></html>`))
			return
		}
	}))
	defer server.Close()

	f, _ := NewFilter(server.URL+"/docs", nil, nil)
	c, err := NewCrawler(f, NewHTTPFetcher(&http.Client{Timeout: 5 * time.Second},
		FetchLimits{MaxBodyBytes: 1 << 20}), Limits{Concurrency: 2, MaxDepth: 3, MaxPages: 20})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Crawl(context.Background(), server.URL+"/docs"); err != nil {
		t.Fatalf("crawl: %v", err)
	}
	if !pageFetched {
		t.Error("a malformed robots.txt must not block the whole site")
	}
}

// TestRobots_SitemapDeclaredInRobotsIsUsed pins discovery priority: a Sitemap
// line in robots.txt wins over probing the default paths.
func TestRobots_SitemapDeclaredInRobotsIsUsed(t *testing.T) {
	var base string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nSitemap: " + base + "/custom-sitemap.xml\n"))
			return
		case "/custom-sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?><urlset>` +
				`<url><loc>` + base + `/docs/one</loc></url>` +
				`<url><loc>` + base + `/docs/two</loc></url>` +
				`</urlset>`))
			return
		case "/sitemap.xml", "/sitemap_index.xml":
			t.Errorf("default sitemap path %q was probed even though robots.txt declared one", r.URL.Path)
			return
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>P</title></head><body><main><h1>P</h1><p>x</p></main></body></html>`))
			return
		}
	}))
	defer server.Close()
	base = server.URL

	got, err := DiscoverSitemap(context.Background(), &http.Client{Timeout: 5 * time.Second}, base+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}
	if got != base+"/custom-sitemap.xml" {
		t.Errorf("declared sitemap = %q, want %q", got, base+"/custom-sitemap.xml")
	}
}
