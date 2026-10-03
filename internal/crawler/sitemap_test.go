package crawler_test

import (
	"testing"

	"github.com/nmdra/docmcp/internal/crawler"
)

func TestParseSitemap_URLSet(t *testing.T) {
	raw := readFixture(t, "urlset.xml")

	got, err := crawler.ParseSitemap(raw)
	if err != nil {
		t.Fatalf("ParseSitemap: %v", err)
	}

	want := []crawler.SitemapURL{
		{Loc: "https://example.com/docs/", LastMod: "2026-09-01"},
		{Loc: "https://example.com/docs/install", LastMod: "2026-09-02T10:30:00+02:00"},
		{Loc: "https://example.com/docs/api", LastMod: "2026-09-03"},
	}

	if len(got) != len(want) {
		t.Fatalf("parsed %d URLs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("URL[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseSitemap_Index(t *testing.T) {
	raw := readFixture(t, "index.xml")

	got, err := crawler.ParseSitemapIndex(raw)
	if err != nil {
		t.Fatalf("ParseSitemapIndex: %v", err)
	}

	want := []crawler.SitemapEntry{
		{Loc: "https://example.com/sitemap-docs.xml", LastMod: "2026-09-01"},
		{Loc: "https://example.com/sitemap-api.xml", LastMod: "2026-09-02"},
	}

	if len(got) != len(want) {
		t.Fatalf("parsed %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseSitemap_LastModified(t *testing.T) {
	raw := readFixture(t, "urlset.xml")

	got, err := crawler.ParseSitemap(raw)
	if err != nil {
		t.Fatalf("ParseSitemap: %v", err)
	}

	var (
		dateOnly string
		fullTime string
	)
	for _, u := range got {
		switch u.Loc {
		case "https://example.com/docs/":
			dateOnly = u.LastMod
		case "https://example.com/docs/install":
			fullTime = u.LastMod
		}
	}

	if dateOnly != "2026-09-01" {
		t.Errorf("date-only lastmod = %q, want 2026-09-01", dateOnly)
	}
	if fullTime != "2026-09-02T10:30:00+02:00" {
		t.Errorf("full-timestamp lastmod = %q, want 2026-09-02T10:30:00+02:00", fullTime)
	}
}

func TestParseSitemap_Malformed(t *testing.T) {
	raw := readFixture(t, "malformed.xml")

	if _, err := crawler.ParseSitemap(raw); err == nil {
		t.Error("ParseSitemap on malformed XML succeeded, want error")
	}
}

func TestParseSitemap_Empty(t *testing.T) {
	raw := readFixture(t, "empty.xml")

	got, err := crawler.ParseSitemap(raw)
	if err != nil {
		t.Fatalf("ParseSitemap on empty urlset: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parsed %d URLs from empty sitemap, want 0", len(got))
	}
}

func TestParseSitemap_WrongDocumentTypeIsRejected(t *testing.T) {
	if _, err := crawler.ParseSitemap(readFixture(t, "index.xml")); err == nil {
		t.Error("ParseSitemap on a sitemap index succeeded, want error")
	}
	if _, err := crawler.ParseSitemapIndex(readFixture(t, "urlset.xml")); err == nil {
		t.Error("ParseSitemapIndex on a urlset succeeded, want error")
	}
}
