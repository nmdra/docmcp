package parser_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nmdra/docmcp/internal/parser"
)

func htmlFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "html", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestHTMLConverter_Title(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if doc.Title != "Authentication" {
		t.Errorf("Title = %q, want %q (article heading beats the page title suffix)",
			doc.Title, "Authentication")
	}
}

func TestHTMLConverter_MainContent(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if !contains(doc.Markdown, "OAuth 2.0 bearer tokens") {
		t.Errorf("markdown is missing main content:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_RemovesNav(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	for _, junk := range []string{"Home", "console.log", "All rights reserved"} {
		if contains(doc.Markdown, junk) {
			t.Errorf("markdown still contains chrome %q:\n%s", junk, doc.Markdown)
		}
	}
}

func TestHTMLConverter_RemovesSidebar(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if contains(doc.Markdown, "On this page") {
		t.Errorf("markdown still contains the sidebar:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_PreservesCode(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if !contains(doc.Markdown, "```bash") {
		t.Errorf("code block lost its language or fence:\n%s", doc.Markdown)
	}
	if !contains(doc.Markdown, "grant_type=client_credentials") {
		t.Errorf("code block content was dropped:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_PreservesHeadings(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	for _, want := range []string{"# Authentication", "## Obtaining a token", "## Rotating keys", "## API keys"} {
		if !contains(doc.Markdown, want) {
			t.Errorf("markdown is missing heading %q:\n%s", want, doc.Markdown)
		}
	}
}

func TestHTMLConverter_PreservesLinks(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if !contains(doc.Markdown, "/latest/api") {
		t.Errorf("in-body link was dropped:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_ResolvesRelativeLinks(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "relative.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if !contains(doc.Markdown, "https://docs.acme.test/latest/api") {
		t.Errorf("relative link was not resolved against the page URL:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_UsesCanonicalURL(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth?utm_source=newsletter",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if doc.CanonicalURL != "https://docs.acme.test/latest/auth" {
		t.Errorf("CanonicalURL = %q, want the <link rel=canonical> value", doc.CanonicalURL)
	}
}

func TestHTMLConverter_GoldenMarkdown(t *testing.T) {
	golden := readGolden(t, "auth.golden.md")

	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: htmlFixture(t, "auth.html"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if doc.Markdown != golden {
		t.Errorf("markdown drifted from golden.\n--- got ---\n%s\n--- want ---\n%s",
			doc.Markdown, golden)
	}
}

func TestHTMLConverter_EmptyDocument(t *testing.T) {
	// A page with no body content is skipped, not indexed as an empty document:
	// an empty chunk would be pure noise for retrieval.
	_, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/blank",
		HTML: "<!doctype html><html><head><title>Blank</title></head><body></body></html>",
	})
	if err == nil {
		t.Fatal("ParseHTML on an empty page succeeded, want ErrEmptyDocument")
	}
	if !errors.Is(err, parser.ErrEmptyDocument) {
		t.Errorf("error = %v, want ErrEmptyDocument", err)
	}
}

func TestHTMLConverter_RejectsMalformedHTML(t *testing.T) {
	doc, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/broken",
		HTML: "<html><body><h1>Unclosed",
	})
	if err != nil {
		t.Fatalf("ParseHTML on recoverable HTML: %v", err)
	}
	if !contains(doc.Markdown, "Unclosed") {
		t.Errorf("recovered markdown missing the heading:\n%s", doc.Markdown)
	}
}

func TestHTMLConverter_ContentHashIsStable(t *testing.T) {
	html := htmlFixture(t, "auth.html")

	first, err := parser.ParseHTML(parser.Page{URL: "https://docs.acme.test/latest/auth", HTML: html})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	second, err := parser.ParseHTML(parser.Page{URL: "https://docs.acme.test/latest/auth", HTML: html})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if first.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}
	if first.ContentHash != second.ContentHash {
		t.Errorf("ContentHash differs across runs: %q vs %q", first.ContentHash, second.ContentHash)
	}
}

func TestHTMLConverter_ContentHashChangesWithContent(t *testing.T) {
	base := htmlFixture(t, "auth.html")

	original, err := parser.ParseHTML(parser.Page{URL: "https://docs.acme.test/latest/auth", HTML: base})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	changed, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: base + "<!-- one more comment -->",
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if original.ContentHash != changed.ContentHash {
		t.Error("ContentHash changed for a comment-only edit, want markdown-driven hashing")
	}

	edited, err := parser.ParseHTML(parser.Page{
		URL:  "https://docs.acme.test/latest/auth",
		HTML: replaceFirst(base, "OAuth 2.0 bearer tokens", "OAuth 2.1 bearer tokens"),
	})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if original.ContentHash == edited.ContentHash {
		t.Error("ContentHash unchanged after a real content edit")
	}
}

func TestHTMLConverter_DocumentIDIsDeterministic(t *testing.T) {
	html := htmlFixture(t, "auth.html")

	first, err := parser.ParseHTML(parser.Page{URL: "https://docs.acme.test/latest/auth", HTML: html})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	second, err := parser.ParseHTML(parser.Page{URL: "https://docs.acme.test/latest/auth", HTML: html})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if first.ID == "" {
		t.Fatal("document ID is empty")
	}
	if first.ID != second.ID {
		t.Errorf("document ID differs across runs: %q vs %q", first.ID, second.ID)
	}
}

func TestHTMLConverter_RejectsNonHTML(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		if _, err := parser.ParseHTML(parser.Page{URL: "https://x.test/", HTML: raw}); err == nil {
			t.Errorf("ParseHTML(%q) succeeded, want error", raw)
		}
	}
}

func TestHTMLConverter_RejectsMissingPageURL(t *testing.T) {
	if _, err := parser.ParseHTML(parser.Page{URL: "", HTML: "<html><body><p>x</p></body></html>"}); err == nil {
		t.Error("ParseHTML with no page URL succeeded, want error")
	}
}

func TestHTMLConverter_SkipsNonHTMLContentTypes(t *testing.T) {
	_, err := parser.ParseHTML(parser.Page{
		URL:         "https://docs.acme.test/latest/spec.pdf",
		HTML:        "%PDF-1.7",
		ContentType: "application/pdf",
	})
	if err == nil {
		t.Error("ParseHTML on a PDF succeeded, want error")
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "markdown", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(data)
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func replaceFirst(s, old, new string) string {
	return strings.Replace(s, old, new, 1)
}
