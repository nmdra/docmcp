package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/docmcp/docmcp/internal/chunker"
	"github.com/docmcp/docmcp/internal/config"
	"github.com/docmcp/docmcp/internal/crawler"
	"github.com/docmcp/docmcp/internal/embedding"
	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/source"
	"github.com/docmcp/docmcp/internal/store"
)

// Runtime is the assembled application: the real crawler, parser, chunker,
// embedder, store, and source repository for one configuration.
//
// Everything a command needs hangs off this, so a command never reaches for a
// concrete type it was not given.
type Runtime struct {
	Config   config.Config
	Sources  source.Repository
	Store    store.Store
	Embedder *embedding.Service
	Ingestor Ingestor
}

// Ingestor is the ingestion surface a command needs. It is an interface so a
// command cannot reach past the service into the store or the crawler.
type Ingestor interface {
	Ingest(ctx context.Context, src source.Source) (ingest.Report, error)
	Remove(ctx context.Context, sourceID string) error
}

// Close releases the store. The CLI defers this once per run.
func (r *Runtime) Close() error {
	if r == nil || r.Store == nil {
		return nil
	}
	return r.Store.Close()
}

// NewRuntime assembles the application for cfg. Every path comes from cfg, so a
// test that injects a temp data directory never touches real user state.
func NewRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	sources, err := source.NewFileRepository(cfg.SourcesPath())
	if err != nil {
		return nil, fmt.Errorf("open sources: %w", err)
	}

	chromaStore, err := store.NewChromaStore(ctx, store.ChromaConfig{Path: cfg.ChromaPath()})
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}

	embedder, err := newEmbedder(cfg)
	if err != nil {
		chromaStore.Close()
		return nil, err
	}

	markdownChunker, err := chunker.NewMarkdownChunker()
	if err != nil {
		chromaStore.Close()
		return nil, fmt.Errorf("build chunker: %w", err)
	}

	rt := &Runtime{
		Config:   cfg,
		Sources:  sources,
		Store:    chromaStore,
		Embedder: embedder,
	}

	// The crawler is built per source, because its scope comes from that source's
	// base URL and include/exclude rules.
	rt.Ingestor = &lazyIngestor{rt: rt, ch: markdownChunker}

	return rt, nil
}

// lazyIngestor builds the crawler for whichever source is being ingested. The
// crawler is scope-bound, so it cannot be constructed once for all sources.
type lazyIngestor struct {
	rt *Runtime
	ch chunker.Chunker
}

func (l *lazyIngestor) Ingest(ctx context.Context, src source.Source) (ingest.Report, error) {
	c, err := l.crawlerFor(src)
	if err != nil {
		return ingest.Report{}, err
	}

	ingestor, err := newIngestor(c, l.ch, l.rt.Store, l.rt.Embedder)
	if err != nil {
		return ingest.Report{}, err
	}
	return ingestor.Ingest(ctx, src)
}

func (l *lazyIngestor) Remove(ctx context.Context, sourceID string) error {
	ingestor, err := newIngestor(nil, l.ch, l.rt.Store, l.rt.Embedder)
	if err != nil {
		return err
	}
	return ingestor.Remove(ctx, sourceID)
}

func (l *lazyIngestor) crawlerFor(src source.Source) (*crawler.Crawler, error) {
	filter, err := crawler.NewFilter(src.BaseURL, src.Includes, src.Excludes)
	if err != nil {
		return nil, fmt.Errorf("crawl scope for %s: %w", src.LibraryID, err)
	}

	client := newHTTPClient(l.rt.Config.Crawler.RequestTimeout)
	fetcher := crawler.NewHTTPFetcher(client, crawler.FetchLimits{
		Timeout:      l.rt.Config.Crawler.RequestTimeout,
		MaxBodyBytes: l.rt.Config.Crawler.MaxBodyBytes,
	})

	c, err := crawler.NewCrawler(filter, fetcher, crawler.Limits{
		Concurrency: l.rt.Config.Crawler.Concurrency,
		MaxPages:    l.rt.Config.Crawler.MaxPages,
		MaxDepth:    l.rt.Config.Crawler.MaxDepth,
		RateLimit:   l.rt.Config.Crawler.RateLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("build crawler for %s: %w", src.LibraryID, err)
	}
	return c, nil
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// newEmbedder builds the configured provider. The default provider is not
// implemented in v0.1; it reports that clearly rather than silently falling back
// to something else.
func newEmbedder(cfg config.Config) (*embedding.Service, error) {
	switch cfg.Embedding.Provider {
	case "ollama":
		provider, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
			BaseURL: cfg.Embedding.BaseURL,
			Model:   cfg.Embedding.Model,
			Timeout: cfg.Crawler.RequestTimeout,
		})
		if err != nil {
			return nil, err
		}
		return embedding.NewService(provider), nil

	case "openai":
		provider, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			BaseURL: cfg.Embedding.BaseURL,
			Model:   cfg.Embedding.Model,
			APIKey:  cfg.Embedding.APIKey,
			Timeout: cfg.Crawler.RequestTimeout,
		})
		if err != nil {
			return nil, err
		}
		return embedding.NewService(provider), nil

	default:
		return nil, fmt.Errorf(
			"the built-in %q embedder is not available yet; "+
				"set embedding.provider to \"ollama\" or \"openai\" in %s",
			cfg.Embedding.Provider, config.DefaultPath())
	}
}
