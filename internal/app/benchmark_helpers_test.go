package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nmdra/docmcp/internal/chunker"
	"github.com/nmdra/docmcp/internal/embedding"
	"github.com/nmdra/docmcp/internal/ingest"
	"github.com/nmdra/docmcp/internal/search"
	"github.com/nmdra/docmcp/internal/store"
)

// benchmarkLibraryID is the single library the whole benchmark corpus lives in.
const benchmarkLibraryID = "/local/bench/1"

const benchmarkHost = "https://bench.test"

// benchmarkEngine indexes the synthetic corpus with the real embedder and returns
// an engine pointed at it, plus the corpus so a test can tell which topic a hit
// belongs to.
//
// It runs against real Chroma and the real model on purpose: a benchmark measured
// with fakes would only prove the fakes still work.
func benchmarkEngine(t *testing.T) (*search.Engine, []benchmarkDoc) {
	t.Helper()

	ctx := t.Context()
	docs := benchmarkCorpus()
	dataDir := t.TempDir()

	st, err := store.NewChromaStore(ctx, store.ChromaConfig{
		Path: filepath.Join(dataDir, "chroma"),
	})
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	// The model comes from the shared machine cache, not the per-test data dir.
	embedder, err := embedding.NewLocalEmbedder(embedding.LocalConfig{
		CacheDir: embedding.DefaultCacheDir(),
	})
	if err != nil {
		t.Fatalf("build embedder: %v", err)
	}
	service := embedding.NewService(embedder)

	markdownChunker, err := chunker.NewMarkdownChunker()
	if err != nil {
		t.Fatalf("build chunker: %v", err)
	}

	locator := chunker.Locator{
		SourceID:  "bench",
		LibraryID: benchmarkLibraryID,
		Version:   "1",
	}

	var (
		chunks []store.Chunk
		texts  []string
	)

	for _, doc := range docs {
		url := benchmarkHost + doc.path

		produced, err := markdownChunker.Chunk(chunker.Document{
			ID:           doc.path,
			URL:          url,
			CanonicalURL: url,
			Title:        doc.title,
			Markdown:     doc.md,
			ContentHash:  doc.md,
		}, locator)
		if err != nil {
			t.Fatalf("chunk %s: %v", doc.path, err)
		}

		for _, c := range produced {
			chunks = append(chunks, store.Chunk{
				ID:          c.ID,
				SourceID:    c.SourceID,
				LibraryID:   c.LibraryID,
				Version:     c.Version,
				DocumentID:  c.DocumentID,
				URL:         c.URL,
				Title:       c.Title,
				HeadingPath: c.HeadingPath,
				Index:       c.Index,
				Content:     c.Content,
				ContentHash: c.ContentHash,
			})
			texts = append(texts, ingest.EmbeddingText(ingest.Chunk{
				Title: c.Title, HeadingPath: c.HeadingPath, Content: c.Content,
			}))
		}
	}

	if len(chunks) == 0 {
		t.Fatal("the benchmark corpus produced no chunks")
	}

	vectors, err := service.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("embed corpus: %v", err)
	}
	if err := st.UpsertChunks(ctx, chunks, vectors); err != nil {
		t.Fatalf("store corpus: %v", err)
	}

	engine, err := search.NewEngine(st, service, search.Options{
		CandidateCount: 20,
		FinalChunks:    6,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	return engine, docs
}

// benchmarkHits runs one query and returns its ranked results, trimmed to k.
func benchmarkHits(engine *search.Engine, query string, k int) []search.Result {
	results, err := engine.Search(context.Background(), search.Request{
		LibraryID: benchmarkLibraryID,
		Query:     query,
	})
	if err != nil {
		return nil
	}
	if len(results) > k {
		return results[:k]
	}
	return results
}

// topicsOf maps retrieved chunks back to corpus topics so a hit is scored by
// topic rather than by exact chunk.
func topicsOf(results []search.Result, docs []benchmarkDoc) []string {
	byPath := map[string]string{}
	for _, d := range docs {
		byPath[benchmarkHost+d.path] = d.topic
	}

	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, byPath[r.Chunk.URL])
	}
	return out
}

func anyHitsTopic(hits []search.Result, docs []benchmarkDoc, topic string) bool {
	for _, got := range topicsOf(hits, docs) {
		if got == topic {
			return true
		}
	}
	return false
}

// benchmarkRecall is the fraction of queries whose top-k results include at least
// one chunk from the wanted topic.
func benchmarkRecall(engine *search.Engine, docs []benchmarkDoc, k int) float64 {
	queries := benchmarkQueries()
	if len(queries) == 0 {
		return 0
	}

	hit := 0
	for _, q := range queries {
		if anyHitsTopic(benchmarkHits(engine, q.text, k), docs, q.topic) {
			hit++
		}
	}
	return float64(hit) / float64(len(queries))
}

// benchmarkGoldAccuracy measures answers on the declared gold page, rather
// than crediting an arbitrary page within the same broad topic.
func benchmarkGoldAccuracy(engine *search.Engine, k int) float64 {
	queries := benchmarkQueries()
	if len(queries) == 0 {
		return 0
	}
	hitCount := 0
	for _, q := range queries {
		for _, result := range benchmarkHits(engine, q.text, k) {
			if result.Chunk.URL == benchmarkHost+q.gold {
				hitCount++
				break
			}
		}
	}
	return float64(hitCount) / float64(len(queries))
}

// benchmarkMRR is the mean reciprocal rank of the first relevant chunk, which is
// what decides whether an agent reads the top result or keeps scrolling.
func benchmarkMRR(engine *search.Engine, queries []benchmarkQuery) float64 {
	if len(queries) == 0 {
		return 0
	}

	total := 0.0
	for _, q := range queries {
		gold := benchmarkHost + q.gold

		for i, r := range benchmarkHits(engine, q.text, 20) {
			if r.Chunk.URL == gold {
				total += 1 / float64(i+1)
				break
			}
		}
	}

	return total / float64(len(queries))
}

// requireLocalProvider skips unless the real-model suite was requested. The
// benchmark is meaningless with a fake embedder: it would measure the fake.
func requireLocalProvider(t *testing.T) {
	t.Helper()

	if os.Getenv("DOCMCP_TEST_LOCAL") != "1" {
		t.Skip("set DOCMCP_TEST_LOCAL=1 to run the retrieval benchmark")
	}
}
