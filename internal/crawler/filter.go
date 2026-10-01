package crawler

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Filter decides whether a URL belongs to a source's crawl scope. Order is
// fixed: host, then base path prefix, then includes, then excludes — with
// exclude always winning.
type Filter struct {
	scheme   string
	host     string
	basePath string

	includes []string
	excludes []string
}

// NewFilter builds a scope filter from a source base URL and its glob rules.
// Rules match the URL path, not the full URL.
func NewFilter(baseURL string, includes, excludes []string) (*Filter, error) {
	normalized, err := NormalizeURL(baseURL, "")
	if err != nil {
		return nil, fmt.Errorf("base URL: %w", err)
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("base URL: %w", err)
	}

	return &Filter{
		scheme:   parsed.Scheme,
		host:     parsed.Host,
		basePath: parsed.Path,
		includes: normalizeRules(includes),
		excludes: normalizeRules(excludes),
	}, nil
}

// baseURL reconstructs the source's entry point, used to locate robots.txt.
func (f *Filter) baseURL() (string, error) {
	return f.scheme + "://" + f.host + "/", nil
}

// Allowed reports whether raw falls inside the source's scope. Unparseable or
// non-HTTP URLs are outside scope by definition.
func (f *Filter) Allowed(raw string) bool {
	normalized, err := NormalizeURL(raw, "")
	if err != nil {
		return false
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return false
	}

	// An http:// URL for an https:// source is a downgrade, not the same host.
	if parsed.Scheme != f.scheme || parsed.Host != f.host {
		return false
	}
	if !underBasePath(f.basePath, parsed.Path) {
		return false
	}
	if len(f.includes) > 0 && !matchesAny(f.includes, parsed.Path) {
		return false
	}
	return !matchesAny(f.excludes, parsed.Path)
}

// underBasePath treats the base as a directory prefix, so base "/docs/latest"
// does not admit "/docs/latestness".
func underBasePath(base, path string) bool {
	if base == "" {
		return true
	}
	if path == base {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(base, "/")+"/")
}

func matchesAny(rules []string, path string) bool {
	for _, rule := range rules {
		if ok, err := doublestar.Match(rule, path); err == nil && ok {
			return true
		}
		// A rule written as a bare directory prefix still matches its subtree.
		if strings.HasSuffix(rule, "/**") {
			prefix := strings.TrimSuffix(rule, "**")
			if path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}

func normalizeRules(rules []string) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !strings.HasPrefix(r, "/") {
			r = "/" + r
		}
		out = append(out, r)
	}
	return out
}
