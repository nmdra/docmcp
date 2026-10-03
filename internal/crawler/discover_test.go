package crawler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nmdra/docmcp/internal/crawler"
)

func TestDiscoverSitemap_FromRobots(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\nDisallow: /private/\nSitemap: https://example.com/custom-sitemap.xml\n"))
	})
	mux.HandleFunc("/custom-sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><urlset><url><loc>https://example.com/docs/a</loc></url></urlset>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := crawler.DiscoverSitemap(t.Context(), srv.Client(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}

	if want := "https://example.com/custom-sitemap.xml"; got != want {
		t.Errorf("discovered %q, want %q", got, want)
	}
}

func TestDiscoverSitemap_DefaultPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><urlset><url><loc>https://example.com/docs/a</loc></url></urlset>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := crawler.DiscoverSitemap(t.Context(), srv.Client(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}

	if want := srv.URL + "/sitemap.xml"; got != want {
		t.Errorf("discovered %q, want %q", got, want)
	}
}

func TestDiscoverSitemap_IndexFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\n"))
	})
	mux.HandleFunc("/sitemap_index.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><sitemapindex></sitemapindex>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := crawler.DiscoverSitemap(t.Context(), srv.Client(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}

	if want := srv.URL + "/sitemap_index.xml"; got != want {
		t.Errorf("discovered %q, want %q", got, want)
	}
}

func TestDiscoverSitemap_None(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := crawler.DiscoverSitemap(t.Context(), srv.Client(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}
	if got != "" {
		t.Errorf("discovered %q, want empty", got)
	}
}

func TestDiscoverSitemap_PrefersRobotsDeclaration(t *testing.T) {
	var base string

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("User-agent: *\nSitemap: " + base + "/declared.xml\n"))
	})
	mux.HandleFunc("/declared.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><urlset></urlset>`))
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><urlset></urlset>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	got, err := crawler.DiscoverSitemap(t.Context(), srv.Client(), srv.URL+"/docs/")
	if err != nil {
		t.Fatalf("DiscoverSitemap: %v", err)
	}

	if want := srv.URL + "/declared.xml"; got != want {
		t.Errorf("discovered %q, want %q (robots declaration wins)", got, want)
	}
}
