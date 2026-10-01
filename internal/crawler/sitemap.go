package crawler

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// SitemapURL is one <url> entry of a sitemap urlset.
type SitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

// SitemapEntry is one <sitemap> entry of a sitemap index.
type SitemapEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	URLs    []SitemapURL `xml:"url"`
}

type sitemapIndexDoc struct {
	XMLName  xml.Name       `xml:"sitemapindex"`
	Sitemaps []SitemapEntry `xml:"sitemap"`
}

// ParseSitemap reads a <urlset> document. An index document is rejected here so
// callers cannot silently ingest zero URLs from the wrong file.
func ParseSitemap(data []byte) ([]SitemapURL, error) {
	var doc sitemapURLSet
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}

	if doc.XMLName.Local == "sitemapindex" {
		return nil, fmt.Errorf("parse sitemap: document is a sitemapindex, not a urlset")
	}

	urls := make([]SitemapURL, 0, len(doc.URLs))
	for _, u := range doc.URLs {
		loc := strings.TrimSpace(u.Loc)
		if loc == "" {
			continue
		}
		urls = append(urls, SitemapURL{Loc: loc, LastMod: strings.TrimSpace(u.LastMod)})
	}
	return urls, nil
}

// ParseSitemapIndex reads a <sitemapindex> document.
func ParseSitemapIndex(data []byte) ([]SitemapEntry, error) {
	var doc sitemapIndexDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sitemap index: %w", err)
	}

	if doc.XMLName.Local == "urlset" {
		return nil, fmt.Errorf("parse sitemap index: document is a urlset, not a sitemapindex")
	}

	entries := make([]SitemapEntry, 0, len(doc.Sitemaps))
	for _, s := range doc.Sitemaps {
		loc := strings.TrimSpace(s.Loc)
		if loc == "" {
			continue
		}
		entries = append(entries, SitemapEntry{Loc: loc, LastMod: strings.TrimSpace(s.LastMod)})
	}
	return entries, nil
}
