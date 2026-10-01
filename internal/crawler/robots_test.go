package crawler_test

import (
	"testing"

	"github.com/docmcp/docmcp/internal/crawler"
)

func TestRobots_AllowsPath(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte("User-agent: *\nDisallow: /private/\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	for _, path := range []string{"/", "/docs/a", "/private-page", "/public/b"} {
		if !robots.Allowed(path) {
			t.Errorf("Allowed(%q) = false, want true", path)
		}
	}
}

func TestRobots_DisallowsPath(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte("User-agent: *\nDisallow: /private/\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	for _, path := range []string{"/private/", "/private/secret", "/private/a/b/c"} {
		if robots.Allowed(path) {
			t.Errorf("Allowed(%q) = true, want false", path)
		}
	}
}

func TestRobots_SitemapDeclaration(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte(
		"User-agent: *\n" +
			"Disallow: /x\n" +
			"Sitemap: https://example.com/sitemap-a.xml\n" +
			"Sitemap: https://example.com/sitemap-b.xml\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	got := robots.Sitemaps()
	want := []string{
		"https://example.com/sitemap-a.xml",
		"https://example.com/sitemap-b.xml",
	}

	if len(got) != len(want) {
		t.Fatalf("Sitemaps() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Sitemaps()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRobots_MissingDefaultsToAllowed(t *testing.T) {
	robots, err := crawler.ParseRobots(nil)
	if err != nil {
		t.Fatalf("ParseRobots(nil): %v", err)
	}

	for _, path := range []string{"/", "/anything", "/private/secret"} {
		if !robots.Allowed(path) {
			t.Errorf("Allowed(%q) = false, want true when no robots.txt", path)
		}
	}
}

func TestRobots_EmptyAllowDisallowsNothing(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte("User-agent: *\nDisallow:\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	if !robots.Allowed("/private/secret") {
		t.Error("empty Disallow must not block anything")
	}
}

func TestRobots_AllowOverridesLongestDisallow(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte(
		"User-agent: *\n" +
			"Disallow: /docs/\n" +
			"Allow: /docs/public/\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	if !robots.Allowed("/docs/public/intro") {
		t.Error("/docs/public/intro should be allowed")
	}
	if robots.Allowed("/docs/internal/intro") {
		t.Error("/docs/internal/intro should be disallowed")
	}
}

func TestRobots_CrawlDelay(t *testing.T) {
	robots, err := crawler.ParseRobots([]byte("User-agent: *\nCrawl-delay: 2.5\n"))
	if err != nil {
		t.Fatalf("ParseRobots: %v", err)
	}

	if robots.CrawlDelay() != 2.5 {
		t.Errorf("CrawlDelay() = %v, want 2.5", robots.CrawlDelay())
	}
}
