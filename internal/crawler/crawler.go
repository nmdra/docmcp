package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// ErrCrawlLimitExceeded reports that a crawl hit MaxPages and was configured to
// treat that as a failure rather than a partial result.
var ErrCrawlLimitExceeded = errors.New("crawl limit exceeded")

// Limits bounds one crawl.
type Limits struct {
	Concurrency int
	MaxPages    int
	MaxDepth    int

	// RateLimit is requests per second; 0 means unthrottled.
	RateLimit int

	// StopOnLimit makes hitting MaxPages an error instead of a partial crawl.
	StopOnLimit bool
}

// Crawler walks a source's scope: sitemap URLs first, then same-site links from
// the start page. Scope and robots are checked before every fetch, never after.
type Crawler struct {
	filter       *Filter
	fetcher      *HTTPFetcher
	robots       *Robots
	limits       Limits
	limiter      *rate.Limiter
	robotsClient *http.Client
}

func NewCrawler(filter *Filter, fetcher *HTTPFetcher, limits Limits) (*Crawler, error) {
	if filter == nil {
		return nil, fmt.Errorf("crawler: filter is required")
	}
	if fetcher == nil {
		return nil, fmt.Errorf("crawler: fetcher is required")
	}

	if limits.Concurrency <= 0 {
		limits.Concurrency = 4
	}
	if limits.MaxPages <= 0 {
		limits.MaxPages = 1000
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = 20
	}

	c := &Crawler{filter: filter, fetcher: fetcher, limits: limits}
	if limits.RateLimit > 0 {
		c.limiter = rate.NewLimiter(rate.Limit(limits.RateLimit), limits.RateLimit)
	}

	c.loadRobots()
	return c, nil
}

// Crawl returns the pages inside the source scope, deduplicated by canonical URL.
func (c *Crawler) Crawl(ctx context.Context, startURL string) ([]Page, error) {
	start, err := NormalizeURL(startURL, "")
	if err != nil {
		return nil, fmt.Errorf("crawl: %w", err)
	}

	// The start URL is the user's explicit instruction, so it is always an
	// entry point even when no include rule matches it. Everything discovered
	// from it — sitemap entries and links alike — still passes the filter.
	seeds := []workItem{{url: start, depth: 0, seed: true}}
	if discovered := c.discoverCandidates(ctx, start); len(discovered) > 0 {
		for _, u := range discovered {
			seeds = append(seeds, workItem{url: u, depth: 0})
		}
	}

	return c.fetchAll(ctx, seeds, c.limits.MaxDepth)
}

// discoverCandidates prefers sitemap URLs; a site with no sitemap falls back to
// link discovery from the start page.
func (c *Crawler) discoverCandidates(ctx context.Context, start string) []string {
	sitemapURL, err := DiscoverSitemap(ctx, c.fetcher.client, start)
	if err != nil || sitemapURL == "" {
		return nil
	}

	data, err := c.fetchRaw(ctx, sitemapURL)
	if err != nil {
		return nil
	}

	if urls, err := ParseSitemap(data); err == nil && len(urls) > 0 {
		out := make([]string, 0, len(urls))
		for _, u := range urls {
			if normalized, err := NormalizeURL(u.Loc, ""); err == nil {
				out = append(out, normalized)
			}
		}
		return out
	}

	entries, err := ParseSitemapIndex(data)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		child, err := c.fetchRaw(ctx, e.Loc)
		if err != nil {
			continue
		}
		urls, err := ParseSitemap(child)
		if err != nil {
			continue
		}
		for _, u := range urls {
			if normalized, err := NormalizeURL(u.Loc, ""); err == nil {
				out = append(out, normalized)
			}
		}
	}
	return out
}

// fetchAll runs a bounded worker pool over the seeds, expanding by same-scope
// links. The frontier is mutex-guarded and seen is the single deduplication
// point, so two workers can never fetch the same URL.
//
// Shutdown invariant: busy counts workers currently processing an item. When the
// frontier is empty and busy is zero, no further work can appear, so every
// waiting worker exits.
func (c *Crawler) fetchAll(ctx context.Context, seeds []workItem, maxDepth int) ([]Page, error) {
	var (
		mu       sync.Mutex
		cond     = sync.NewCond(&mu)
		frontier []workItem
		seen     = map[string]bool{}
		results  []Page
		busy     int
		capped   bool
	)

	for _, seed := range seeds {
		if seen[seed.url] {
			continue
		}
		seen[seed.url] = true
		frontier = append(frontier, seed)
	}

	var wg sync.WaitGroup
	for range c.limits.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				mu.Lock()
				for len(frontier) == 0 && busy > 0 && !capped {
					cond.Wait()
				}
				if capped || len(frontier) == 0 {
					mu.Unlock()
					return
				}

				item := frontier[0]
				frontier = frontier[1:]
				isSeed := item.seed
				busy++
				mu.Unlock()

				page, ok := c.fetchScoped(ctx, item.url, isSeed)

				mu.Lock()
				if ok && len(results) < c.limits.MaxPages {
					results = append(results, page)
					if item.depth < maxDepth {
						for _, link := range ExtractLinks(page.Body, page.URL) {
							if !c.filter.Allowed(link) || seen[link] {
								continue
							}
							seen[link] = true
							frontier = append(frontier, workItem{url: link, depth: item.depth + 1})
						}
					}
				}
				busy--

				// The cap is checked under the same lock that appends, so no
				// worker can slip past it between the check and the append.
				if len(results) >= c.limits.MaxPages {
					capped = true
				}
				cond.Broadcast()
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if capped && c.limits.StopOnLimit {
		return nil, fmt.Errorf("%w: stopped at %d pages", ErrCrawlLimitExceeded, c.limits.MaxPages)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].URL < results[j].URL })
	return results, nil
}

// workItem is one URL to fetch. A seed is in scope by construction; a
// discovered link must clear the filter.
type workItem struct {
	url   string
	depth int
	seed  bool
}

// fetchScoped is the only path to the network: rate limit, then robots, then
// scope. A disallowed or out-of-scope URL is never fetched.
func (c *Crawler) fetchScoped(ctx context.Context, raw string, isSeed bool) (Page, bool) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return Page{}, false
		}
	}

	if !isSeed && !c.filter.Allowed(raw) {
		return Page{}, false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return Page{}, false
	}
	if !c.robots.Allowed(parsed.Path) {
		return Page{}, false
	}

	page, err := c.fetcher.Fetch(ctx, raw)
	if err != nil {
		return Page{}, false
	}
	if !isDocument(page.ContentType) {
		return Page{}, false
	}
	return page, true
}

// fetchRaw reads a non-HTML resource (robots.txt, sitemap XML) that is not
// itself subject to the document filter.
func (c *Crawler) fetchRaw(ctx context.Context, raw string) ([]byte, error) {
	page, err := c.fetcher.Fetch(ctx, raw)
	if err != nil {
		return nil, err
	}
	return []byte(page.Body), nil
}

// loadRobots reads robots.txt once at construction. It runs on its own client so
// a slow or hostile robots endpoint cannot stall construction.
func (c *Crawler) loadRobots() {
	c.robotsClient = &http.Client{Timeout: 10 * time.Second}

	base, err := c.filter.baseURL()
	if err != nil {
		c.robots = &Robots{}
		return
	}

	resp, err := c.robotsClient.Get(base + "/robots.txt")
	if err != nil {
		c.robots = &Robots{}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.robots = &Robots{}
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		c.robots = &Robots{}
		return
	}

	robots, err := ParseRobots(body)
	if err != nil {
		c.robots = &Robots{}
		return
	}
	c.robots = robots
}

func isDocument(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml", "":
		return true
	default:
		return false
	}
}

// ExtractLinks returns the same-page hrefs of an HTML document, resolved and
// filtered to HTTP(S).
func ExtractLinks(htmlBody, pageURL string) []string {
	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return nil
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key != "href" {
					continue
				}
				if resolved, err := NormalizeURL(attr.Val, base.String()); err == nil {
					out = append(out, resolved)
				}
				break
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return out
}
