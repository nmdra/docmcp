package crawler

import (
	"fmt"
	"net/url"
	"strings"
)

// ErrUnsupportedURL marks a link DocMCP will never fetch.
var ErrUnsupportedURL = fmt.Errorf("unsupported URL")

// KeepQueryParams names the query parameters that carry meaning for a source.
// Anything outside this set is treated as tracking noise and dropped.
type KeepQueryParams []string

var defaultTrackingPrefixes = []string{"utm_"}

var defaultTrackingParams = map[string]struct{}{
	"gclid":       {},
	"fbclid":      {},
	"msclkid":     {},
	"mc_cid":      {},
	"mc_eid":      {},
	"ref_src":     {},
	"igshid":      {},
	"yclid":       {},
	"_ga":         {},
	"_gl":         {},
	"dclid":       {},
	"twclid":      {},
	"si":          {},
	"spm":         {},
	"trk":         {},
	"trkCampaign": {},
}

// NormalizeURL resolves raw against base (base may be empty for absolute input),
// then reduces it to a canonical form: fragments gone, tracking parameters gone,
// scheme and host lowercased, path deduplicated, trailing slash trimmed.
// Unsupported schemes are rejected rather than silently dropped.
func NormalizeURL(raw string, base string, keep ...KeepQueryParams) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty URL", ErrUnsupportedURL)
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %v", ErrUnsupportedURL, raw, err)
	}

	switch strings.ToLower(ref.Scheme) {
	case "", "http", "https":
	default:
		return "", fmt.Errorf("%w: scheme %q in %q", ErrUnsupportedURL, ref.Scheme, raw)
	}

	if base == "" {
		if ref.Scheme == "" {
			return "", fmt.Errorf("%w: %q is relative and no base URL given", ErrUnsupportedURL, raw)
		}
	} else {
		parsedBase, err := url.Parse(base)
		if err != nil {
			return "", fmt.Errorf("%w: bad base %q: %v", ErrUnsupportedURL, base, err)
		}
		ref = parsedBase.ResolveReference(ref)
	}

	var preserved KeepQueryParams
	for _, k := range keep {
		preserved = append(preserved, k...)
	}

	ref.Fragment = ""
	ref.RawFragment = ""
	ref.Scheme = strings.ToLower(ref.Scheme)
	ref.Host = strings.ToLower(ref.Host)
	ref.Path = normalizePath(ref.Path)

	q := ref.Query()
	for key := range q {
		if isTrackingParam(key, preserved) {
			q.Del(key)
		}
	}
	ref.RawQuery = q.Encode()

	return ref.String(), nil
}

func isTrackingParam(key string, keep KeepQueryParams) bool {
	for _, k := range keep {
		if strings.EqualFold(key, k) {
			return false
		}
	}
	if _, ok := defaultTrackingParams[strings.ToLower(key)]; ok {
		return true
	}
	for _, prefix := range defaultTrackingPrefixes {
		if len(key) > len(prefix) && strings.EqualFold(key[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// normalizePath cleans the path and applies one trailing-slash policy: the root
// keeps none, every other path loses its trailing slash.
func normalizePath(path string) string {
	if path == "" || path == "/" {
		return ""
	}

	cleaned := cleanSlashes(path)
	if cleaned == "/" {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}

func cleanSlashes(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}
