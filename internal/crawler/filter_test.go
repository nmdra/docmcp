package crawler_test

import (
	"testing"

	"github.com/nmdra/docmcp/internal/crawler"
)

func newFilter(t *testing.T, base string, includes, excludes []string) *crawler.Filter {
	t.Helper()

	f, err := crawler.NewFilter(base, includes, excludes)
	if err != nil {
		t.Fatalf("NewFilter(%q): %v", base, err)
	}
	return f
}

func TestFilter_AllowsSameHost(t *testing.T) {
	f := newFilter(t, "https://pi.dev/docs/latest/", nil, nil)

	if !f.Allowed("https://pi.dev/docs/latest/mcp") {
		t.Error("same-host URL rejected, want allowed")
	}
}

func TestFilter_RejectsOtherHost(t *testing.T) {
	f := newFilter(t, "https://pi.dev/docs/latest/", nil, nil)

	for _, raw := range []string{
		"https://github.com/pi/pi",
		"https://evil.pi.dev/docs/latest/x",
		"http://pi.dev/docs/latest/mcp",
	} {
		if f.Allowed(raw) {
			t.Errorf("Allowed(%q) = true, want false", raw)
		}
	}
}

func TestFilter_RejectsOutsideBasePath(t *testing.T) {
	f := newFilter(t, "https://pi.dev/docs/latest/", nil, nil)

	for _, raw := range []string{
		"https://pi.dev/blog/why-pi",
		"https://pi.dev/",
		"https://pi.dev/docs/v2/mcp",
		"https://pi.dev/docs/latestness",
	} {
		if f.Allowed(raw) {
			t.Errorf("Allowed(%q) = true, want false", raw)
		}
	}
}

func TestFilter_IncludeMatches(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", []string{"/docs/**"}, nil)

	if !f.Allowed("https://example.com/docs/api/auth") {
		t.Error("/docs/api/auth rejected, want allowed")
	}
}

func TestFilter_IncludeMissingRejects(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", []string{"/docs/api/**"}, nil)

	for _, raw := range []string{
		"https://example.com/docs/guides/start",
		"https://example.com/docs",
	} {
		if f.Allowed(raw) {
			t.Errorf("Allowed(%q) = true, want false", raw)
		}
	}
}

func TestFilter_ExcludeWins(t *testing.T) {
	f := newFilter(t, "https://example.com/docs",
		[]string{"/docs/**"},
		[]string{"/docs/archive/**", "/docs/internal/**"})

	cases := []struct {
		raw  string
		want bool
	}{
		{"https://example.com/docs/api/auth", true},
		{"https://example.com/docs/guides/start", true},
		{"https://example.com/docs/archive/v1", false},
		{"https://example.com/docs/archive/deep/nested/page", false},
		{"https://example.com/docs/internal/spec", false},
		{"https://example.com/docs/internal-notes", true},
	}

	for _, tc := range cases {
		if got := f.Allowed(tc.raw); got != tc.want {
			t.Errorf("Allowed(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestFilter_DoubleStarPattern(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", []string{"/**"}, []string{"/docs/drafts/**"})

	if !f.Allowed("https://example.com/docs/a/b/c") {
		t.Error("/** should match any depth under base")
	}
	if f.Allowed("https://example.com/docs/drafts/x/y") {
		t.Error("exclude should win over /**")
	}
}

func TestFilter_EmptyIncludesAcceptsWholeBasePath(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", nil, nil)

	if !f.Allowed("https://example.com/docs/anything/deep") {
		t.Error("no include rules should accept everything under the base path")
	}
}

func TestFilter_NormalizesBeforeMatching(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", []string{"/docs/**"}, nil)

	if !f.Allowed("https://EXAMPLE.com/docs/api?utm_source=x#top") {
		t.Error("tracking params and fragment should not change the filter decision")
	}
}

func TestFilter_RejectsUnparseableBase(t *testing.T) {
	if _, err := crawler.NewFilter("mailto:x@example.com", nil, nil); err == nil {
		t.Error("NewFilter with non-http base succeeded, want error")
	}
}

func TestFilter_RejectsNonHTTPURL(t *testing.T) {
	f := newFilter(t, "https://example.com/docs", nil, nil)

	if f.Allowed("file:///etc/passwd") {
		t.Error("file:// URL allowed, want false")
	}
}
