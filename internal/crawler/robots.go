package crawler

import (
	"strings"

	"github.com/temoto/robotstxt"
)

// Robots is a parsed robots.txt. A source with no robots.txt allows everything:
// the crawler still asks, and never silently bypasses an explicit Disallow.
type Robots struct {
	data  *robotstxt.RobotsData
	agent string
}

// ParseRobots reads robots.txt content. Empty or absent content yields a
// permissive Robots.
func ParseRobots(content []byte) (*Robots, error) {
	r := &Robots{agent: "DocMCP"}

	if len(strings.TrimSpace(string(content))) == 0 {
		return r, nil
	}

	parsed, err := robotstxt.FromBytes(content)
	if err != nil {
		// An unparseable robots.txt is treated as absent rather than blocking
		// the crawl outright; the source's own filter still bounds the scope.
		return r, nil
	}

	r.data = parsed
	return r, nil
}

// Allowed reports whether the path may be fetched. A nil Robots — or one
// parsed from an absent file — allows everything.
func (r *Robots) Allowed(path string) bool {
	if r == nil || r.data == nil {
		return true
	}
	return r.data.TestAgent(path, r.agent)
}

// Sitemaps returns declared sitemap URLs in file order. The first entry is the
// one discovery prefers.
func (r *Robots) Sitemaps() []string {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data.Sitemaps
}

// CrawlDelay returns the declared delay in seconds, or 0 when absent.
func (r *Robots) CrawlDelay() float64 {
	if r == nil || r.data == nil {
		return 0
	}
	group := r.data.FindGroup(r.agent)
	if group == nil {
		return 0
	}
	return group.CrawlDelay.Seconds()
}
