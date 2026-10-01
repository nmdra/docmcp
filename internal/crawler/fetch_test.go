package crawler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docmcp/docmcp/internal/crawler"
)

func newFetcher(t *testing.T, limits crawler.FetchLimits) *crawler.HTTPFetcher {
	t.Helper()

	client := &http.Client{Timeout: limits.Timeout}
	return crawler.NewHTTPFetcher(client, limits)
}

func TestFetcher_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><h1>Install</h1></body></html>"))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/docs/install")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL != srv.URL+"/docs/install" {
		t.Errorf("URL = %q, want %q", page.URL, srv.URL+"/docs/install")
	}
	if !strings.Contains(page.Body, "Install") {
		t.Errorf("Body = %q, want it to contain Install", page.Body)
	}
	if page.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", page.StatusCode)
	}
}

func TestFetcher_ContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/feed")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if want := "application/json"; page.ContentType != want {
		t.Errorf("ContentType = %q, want %q", page.ContentType, want)
	}
}

func TestFetcher_ETag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/a")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", page.ETag, `"abc123"`)
	}
}

func TestFetcher_LastModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/a")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.LastModified != "Wed, 21 Oct 2026 07:28:00 GMT" {
		t.Errorf("LastModified = %q", page.LastModified)
	}
}

func TestFetcher_ConditionalRequestReportsNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte("body"))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/a", crawler.WithValidators(`"abc123"`, ""))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !page.NotModified {
		t.Error("NotModified = false, want true")
	}
}

func TestFetcher_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("late"))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 50 * time.Millisecond, MaxBodyBytes: 1 << 20})

	if _, err := f.Fetch(t.Context(), srv.URL+"/slow"); err == nil {
		t.Error("Fetch succeeded past its timeout, want error")
	}
}

func TestFetcher_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	_, err := f.Fetch(t.Context(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("Fetch of 404 succeeded, want error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want it to mention 404", err)
	}
}

func TestFetcher_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1000})

	if _, err := f.Fetch(t.Context(), srv.URL+"/big"); err == nil {
		t.Error("Fetch of oversized body succeeded, want error")
	}
}

func TestFetcher_Charset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		// 0xE9 is "é" in latin-1.
		w.Write([]byte{'c', 'a', 'f', 0xE9})
	}))
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	page, err := f.Fetch(t.Context(), srv.URL+"/latin")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(page.Body, "café") {
		t.Errorf("Body = %q, want it to decode to café", page.Body)
	}
}

func TestFetcher_RejectsNonHTTPURL(t *testing.T) {
	f := newFetcher(t, crawler.FetchLimits{Timeout: time.Second, MaxBodyBytes: 1 << 20})

	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/x", "mailto:a@b.c"} {
		if _, err := f.Fetch(t.Context(), raw); err == nil {
			t.Errorf("Fetch(%q) succeeded, want error", raw)
		}
	}
}

func TestFetcher_DoesNotFollowRedirectOutsideHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer other.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/secret", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := newFetcher(t, crawler.FetchLimits{Timeout: 5 * time.Second, MaxBodyBytes: 1 << 20})

	if _, err := f.Fetch(t.Context(), srv.URL+"/redirect"); err == nil {
		t.Error("cross-host redirect followed, want error")
	}
}
