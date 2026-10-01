package crawler_test

import (
	"testing"

	"github.com/docmcp/docmcp/internal/crawler"
)

func TestNormalizeURL_RemovesFragment(t *testing.T) {
	got, err := crawler.NormalizeURL("https://example.com/docs/mcp#exposure", "")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if want := "https://example.com/docs/mcp"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeURL_RemovesTrackingParameters(t *testing.T) {
	got, err := crawler.NormalizeURL("https://example.com/docs/a?utm_source=x#intro", "")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if want := "https://example.com/docs/a"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeURL_ResolvesRelativeURL(t *testing.T) {
	base, err := crawler.NormalizeURL("https://example.com/docs/latest/index.html", "")
	if err != nil {
		t.Fatalf("NormalizeURL base: %v", err)
	}

	got, err := crawler.NormalizeURL("../api/auth", base)
	if err != nil {
		t.Fatalf("NormalizeURL relative: %v", err)
	}
	if want := "https://example.com/docs/api/auth"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeURL_NormalizesHostCasing(t *testing.T) {
	got, err := crawler.NormalizeURL("HTTPS://Example.COM/Docs/MCP", "")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if want := "https://example.com/Docs/MCP"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeURL_NormalizesTrailingSlash(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://example.com/docs/mcp/", "https://example.com/docs/mcp"},
		{"https://example.com/docs/mcp", "https://example.com/docs/mcp"},
		{"https://example.com/", "https://example.com"},
		{"https://example.com/docs/", "https://example.com/docs"},
	}

	for _, tc := range cases {
		got, err := crawler.NormalizeURL(tc.in, "")
		if err != nil {
			t.Errorf("NormalizeURL(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeURL_RejectsUnsupportedSchemes(t *testing.T) {
	for _, raw := range []string{
		"mailto:someone@example.com",
		"tel:+15551234",
		"javascript:void(0)",
		"data:text/html,<h1>x</h1>",
		"file:///etc/passwd",
		"ftp://example.com/docs",
		"unix:///tmp/sock",
	} {
		if _, err := crawler.NormalizeURL(raw, ""); err == nil {
			t.Errorf("NormalizeURL(%q) succeeded, want error", raw)
		}
	}
}

func TestNormalizeURL_PreservesMeaningfulQuery(t *testing.T) {
	got, err := crawler.NormalizeURL("https://example.com/docs/a?v=2&utm_medium=y", "")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if want := "https://example.com/docs/a?v=2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeURL_KeepsConfiguredQueryParameters(t *testing.T) {
	keep := crawler.KeepQueryParams{"v", "lang"}

	got, err := crawler.NormalizeURL("https://example.com/docs/a?v=2&lang=de&fbclid=abc", "", keep)
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}

	if got != "https://example.com/docs/a?lang=de&v=2" {
		t.Errorf("got %q, want %q", got, "https://example.com/docs/a?lang=de&v=2")
	}
}

func TestNormalizeURL_DeduplicatesTrackingVariants(t *testing.T) {
	variants := []string{
		"https://example.com/docs/mcp",
		"https://example.com/docs/mcp#tool-exposure",
		"https://example.com/docs/mcp?utm_source=twitter",
		"https://example.com/docs/mcp?utm_campaign=a&utm_content=b",
	}

	want := "https://example.com/docs/mcp"
	for _, raw := range variants {
		got, err := crawler.NormalizeURL(raw, "")
		if err != nil {
			t.Fatalf("NormalizeURL(%q): %v", raw, err)
		}
		if got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
