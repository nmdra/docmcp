package crawler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/net/html/charset"
)

// FetchLimits bounds a single page fetch. MaxBodyBytes keeps one enormous
// response from exhausting memory.
type FetchLimits struct {
	Timeout      time.Duration
	MaxBodyBytes int64
}

// Page is a fetched document, with the validators a later sync can re-check.
type Page struct {
	URL          string
	StatusCode   int
	ContentType  string
	Body         string
	ETag         string
	LastModified string
	NotModified  bool
}

// FetchOption configures a single fetch, such as conditional-request validators.
type FetchOption func(*fetchRequest)

type fetchRequest struct {
	etag         string
	lastModified string
}

// WithValidators sends If-None-Match and If-Modified-Since so an unchanged page
// comes back as a cheap 304 instead of a full body.
func WithValidators(etag, lastModified string) FetchOption {
	return func(r *fetchRequest) {
		r.etag = etag
		r.lastModified = lastModified
	}
}

// HTTPFetcher fetches pages with a hard timeout, a body cap, and no redirects
// across hosts — a redirect off the source's host is a scope escape.
type HTTPFetcher struct {
	client *http.Client
	limits FetchLimits
}

func NewHTTPFetcher(client *http.Client, limits FetchLimits) *HTTPFetcher {
	if client == nil {
		client = &http.Client{}
	}
	if limits.MaxBodyBytes <= 0 {
		limits.MaxBodyBytes = 10 << 20 // 10 MiB
	}
	if limits.Timeout <= 0 {
		limits.Timeout = 20 * time.Second
	}

	return &HTTPFetcher{
		client: &http.Client{
			Timeout: limits.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if req.URL.Host != origin.Host || req.URL.Scheme != origin.Scheme {
					return fmt.Errorf("redirect from %s://%s to %s://%s is outside the allowed host",
						origin.Scheme, origin.Host, req.URL.Scheme, req.URL.Host)
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after %d redirects", len(via))
				}
				return nil
			},
		},
		limits: limits,
	}
}

// Fetch retrieves uri, normalizing it first so the returned Page.URL is the
// canonical form the rest of the pipeline keys on.
func (f *HTTPFetcher) Fetch(ctx context.Context, uri string, opts ...FetchOption) (Page, error) {
	normalized, err := NormalizeURL(uri, "")
	if err != nil {
		return Page{}, err
	}

	req := fetchRequest{}
	for _, opt := range opts {
		opt(&req)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return Page{}, fmt.Errorf("fetch %s: %w", normalized, err)
	}
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	httpReq.Header.Set("User-Agent", userAgent)
	if req.etag != "" {
		httpReq.Header.Set("If-None-Match", req.etag)
	}
	if req.lastModified != "" {
		httpReq.Header.Set("If-Modified-Since", req.lastModified)
	}

	resp, err := f.client.Do(httpReq)
	if err != nil {
		return Page{}, fmt.Errorf("fetch %s: %w", normalized, err)
	}
	defer resp.Body.Close()

	page := Page{
		URL:          finalURL(resp, normalized),
		StatusCode:   resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	if resp.StatusCode == http.StatusNotModified {
		page.NotModified = true
		return page, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Page{}, fmt.Errorf("fetch %s: unexpected status %d", normalized, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.limits.MaxBodyBytes+1))
	if err != nil {
		return Page{}, fmt.Errorf("fetch %s: read body: %w", normalized, err)
	}
	if int64(len(body)) > f.limits.MaxBodyBytes {
		return Page{}, fmt.Errorf("fetch %s: body exceeds %d byte limit", normalized, f.limits.MaxBodyBytes)
	}

	decoded, err := charset.NewReader(bytes.NewReader(body), resp.Header.Get("Content-Type"))
	if err != nil {
		// An unknown or missing charset is not a fetch failure; fall back to
		// the raw bytes so ASCII pages still parse.
		decoded = bytes.NewReader(body)
	}

	text, err := io.ReadAll(decoded)
	if err != nil {
		return Page{}, fmt.Errorf("fetch %s: decode body: %w", normalized, err)
	}
	page.Body = string(text)

	return page, nil
}

const userAgent = "DocMCP/0.1 (+https://github.com/docmcp/docmcp)"

func finalURL(resp *http.Response, fallback string) string {
	if resp.Request == nil || resp.Request.URL == nil {
		return fallback
	}
	if normalized, err := NormalizeURL(resp.Request.URL.String(), ""); err == nil {
		return normalized
	}
	return fallback
}
