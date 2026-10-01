package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// discoverProbeLimit bounds how much of a robots.txt or sitemap response we read
// while sniffing for a sitemap URL.
const discoverProbeLimit = 1 << 20 // 1 MiB

var defaultSitemapPaths = []string{"/sitemap.xml", "/sitemap_index.xml"}

// DiscoverSitemap finds the sitemap URL for a site, following the plan's
// priority order: a Sitemap: line in robots.txt, then /sitemap.xml, then
// /sitemap_index.xml. It returns "" when the site exposes none, which tells the
// crawler to fall back to link discovery.
func DiscoverSitemap(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("sitemap discovery: bad base URL %q: %w", baseURL, err)
	}

	root := &url.URL{Scheme: base.Scheme, Host: base.Host}

	if declared, err := sitemapFromRobots(ctx, client, root); err != nil {
		return "", err
	} else if declared != "" {
		return declared, nil
	}

	for _, path := range defaultSitemapPaths {
		candidate := root.ResolveReference(&url.URL{Path: path}).String()
		ok, err := looksLikeSitemap(ctx, client, candidate)
		if err != nil {
			return "", err
		}
		if ok {
			return candidate, nil
		}
	}

	return "", nil
}

func sitemapFromRobots(ctx context.Context, client *http.Client, root *url.URL) (string, error) {
	robotsURL := root.ResolveReference(&url.URL{Path: "/robots.txt"}).String()

	body, err := fetchProbe(ctx, client, robotsURL)
	if err != nil || body == nil {
		// A missing or unreadable robots.txt is not a discovery failure; the
		// default sitemap paths are still worth probing.
		return "", nil
	}

	robot, err := ParseRobots(body)
	if err != nil {
		return "", err
	}
	return robot.Sitemaps()[0], nil
}

func looksLikeSitemap(ctx context.Context, client *http.Client, candidate string) (bool, error) {
	body, err := fetchProbe(ctx, client, candidate)
	if err != nil {
		return false, err
	}
	if body == nil {
		return false, nil
	}

	head := strings.ToLower(string(body))
	if len(head) > 200 {
		head = head[:200]
	}
	return strings.Contains(head, "<urlset") || strings.Contains(head, "<sitemapindex"), nil
}

func fetchProbe(ctx context.Context, client *http.Client, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("sitemap discovery: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sitemap discovery: GET %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, discoverProbeLimit))
	if err != nil {
		return nil, fmt.Errorf("sitemap discovery: read %s: %w", target, err)
	}
	return body, nil
}
