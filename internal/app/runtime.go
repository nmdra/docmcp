package app

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/docmcp/docmcp/internal/chunker"
	"github.com/docmcp/docmcp/internal/config"
	"github.com/docmcp/docmcp/internal/crawler"
	"github.com/docmcp/docmcp/internal/embedding"
	"github.com/docmcp/docmcp/internal/ingest"
	"github.com/docmcp/docmcp/internal/search"
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

// SearchEngine returns the retrieval engine for this runtime. Both the CLI
// search command and the MCP server use it, so an agent and a user always get
// the same answer to the same question.
func (r *Runtime) SearchEngine() (*search.Engine, error) {
	return search.NewEngine(r.Store, r.Embedder, search.Options{
		CandidateCount: r.Config.Search.CandidateCount,
		FinalChunks:    r.Config.Search.FinalChunks,
	})
}

// Resolver returns a resolver over the given libraries, restricted to those
// that actually have indexed content. Offering an empty library would send an
// agent to a dead end.
func (r *Runtime) Resolver(_ context.Context, libraries []source.Source) *search.Resolver {
	resolver := search.NewResolver(libraries)

	indexed := map[string]int{}
	for _, lib := range libraries {
		if n, err := r.Store.CountChunks(context.Background(), lib.LibraryID); err == nil && n > 0 {
			indexed[lib.LibraryID] = n
		}
	}
	resolver.SetIndexed(indexed)

	return resolver
}

// RequireIndexed reports an error when a library ID is well formed but has no
// indexed content. It is the difference between "no such library" and "that page
// did not match", which an agent cannot otherwise tell apart.
func (r *Runtime) RequireIndexed(ctx context.Context, libraryID string) error {
	count, err := r.Store.CountChunks(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("check library %s: %w", libraryID, err)
	}
	if count == 0 {
		return fmt.Errorf("library %s is not indexed; add it with `docmcp add`", libraryID)
	}
	return nil
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

	case "fake":
		// Test-only: a deterministic in-process embedder, selectable solely by a
		// config that names this provider. It lets the CLI be tested without a
		// model and produces identical vectors for identical text.
		return embedding.NewService(newFakeEmbedder(0)), nil

	default:
		return nil, fmt.Errorf(
			"the built-in %q embedder is not available yet; "+
				"set embedding.provider to \"ollama\" or \"openai\" in %s",
			cfg.Embedding.Provider, config.DefaultPath())
	}
}

// fakeEmbedder is a deterministic in-process embedder used by tests.
//
// It is lexical, not semantic: text is projected onto a fixed-width bag-of-words
// vector, so overlapping wording produces a nearer vector. That is enough to
// exercise the whole retrieval path — ordering, deduplication, the result
// budget, version filtering — offline and identically on every run.
//
// It is not a stand-in for a real model's semantic quality. Judging whether a
// query retrieves the *right* section for paraphrased wording needs a real
// embedder, which is why that check lives in the opt-in provider tests.
type fakeEmbedder struct {
	dim int
}

func newFakeEmbedder(dim int) *fakeEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &fakeEmbedder{dim: dim}
}

// Embed projects each text onto a bag-of-words vector. Identical text always
// yields an identical vector, and a query sharing words with a chunk lands
// closer to it than one that does not.
func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = f.vector(text)
	}
	return out, nil
}

func (f *fakeEmbedder) vector(text string) []float32 {
	v := make([]float32, f.dim)

	for _, word := range strings.Fields(strings.ToLower(text)) {
		hash := fnv32(word)
		v[hash%uint32(f.dim)] += 1
	}

	return normalize(v)
}

// normalize scales a vector to unit length so cosine similarity is meaningful.
func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}

	scale := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= scale
	}
	return v
}

// fnv32 hashes a word to a bucket. FNV is chosen because it is stable across runs
// and platforms: a randomized hash would make retrieval order non-reproducible.
func fnv32(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)

	hash := uint32(offset)
	for i := range len(s) {
		hash ^= uint32(s[i])
		hash *= prime
	}
	return hash
}

func (f *fakeEmbedder) Provider() string { return "fake" }
func (f *fakeEmbedder) Model() string    { return "fake-model" }
func (f *fakeEmbedder) Dimensions() int  { return f.dim }
