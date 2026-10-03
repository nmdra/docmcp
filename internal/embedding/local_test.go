package embedding_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/nmdra/docmcp/internal/embedding"
)

// newLocalEmbedder builds the built-in local embedder after the opt-in gate.
// An explicitly requested provider check must fail if initialization fails.
func newLocalEmbedder(t *testing.T, cacheDir string) *embedding.LocalEmbedder {
	t.Helper()

	e, err := embedding.NewLocalEmbedder(embedding.LocalConfig{
		CacheDir: cacheDir,
		Timeout:  2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("local embedder initialization failed: %v", err)
	}
	return e
}

// TestLocalEmbedder_EmbedsSingleText is opt-in: it needs a real ONNX model, which
// the default suite must not download. Run it with:
//
//	DOCMCP_TEST_LOCAL=1 go test -tags=provider ./internal/embedding/
func TestLocalEmbedder_EmbedsSingleText(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	got, err := e.Embed(t.Context(), []string{"Bearer tokens authenticate every request."})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d vectors, want 1", len(got))
	}
	if len(got[0]) == 0 {
		t.Fatal("vector is empty")
	}
}

func TestLocalEmbedder_EmbedsBatch(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	texts := []string{"first document about routing", "second about caching", "third about sessions"}
	got, err := e.Embed(t.Context(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("got %d vectors for %d texts", len(got), len(texts))
	}
	for i, v := range got {
		if len(v) != len(got[0]) {
			t.Fatalf("vector %d has width %d, want %d", i, len(v), len(got[0]))
		}
	}
}

func TestLocalEmbedder_DimensionsAreStable(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	first, err := e.Embed(t.Context(), []string{"a document"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	second, err := e.Embed(t.Context(), []string{"a different document"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(first[0]) != len(second[0]) {
		t.Errorf("width changed between calls: %d then %d", len(first[0]), len(second[0]))
	}
	if e.Dimensions() != len(first[0]) {
		t.Errorf("Dimensions() = %d, want the observed width %d", e.Dimensions(), len(first[0]))
	}
}

func TestLocalEmbedder_SameInputSameOutput(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	first, err := e.Embed(t.Context(), []string{"determinism check"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	second, err := e.Embed(t.Context(), []string{"determinism check"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if !sameVector(first[0], second[0]) {
		t.Error("the same text produced different vectors; indexing would not be reproducible")
	}
}

func TestLocalEmbedder_VectorsAreFinite(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	got, err := e.Embed(t.Context(), []string{"finite check", "another"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	for i, v := range got {
		for j, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				t.Fatalf("vector %d has a non-finite value at %d", i, j)
			}
		}
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			t.Errorf("vector %d is all zeroes", i)
		}
	}
}

func TestLocalEmbedder_SimilarTextScoresCloser(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	anchor := "Rotate API keys before revoking the old key to avoid downtime."
	near := "Key rotation: create the replacement key first, then revoke the previous one."
	far := "The HTTP server binds to port 8080 and serves static assets from disk."

	vectors, err := e.Embed(t.Context(), []string{anchor, near, far})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	nearScore := cosine(vectors[0], vectors[1])
	farScore := cosine(vectors[0], vectors[2])

	if nearScore <= farScore {
		t.Errorf("near text scored %v, far text scored %v; want near > far",
			nearScore, farScore)
	}
}

func TestLocalEmbedder_IdentityIsTheKnownModel(t *testing.T) {
	requireLocalProvider(t)

	e := newLocalEmbedder(t, localCacheDir(t))

	if e.Provider() != "default" {
		t.Errorf("Provider() = %q, want default", e.Provider())
	}
	if e.Model() != embedding.DefaultEmbeddingModel {
		t.Errorf("Model() = %q, want %q", e.Model(), embedding.DefaultEmbeddingModel)
	}
}

func TestLocalEmbedder_RejectsEmptyCacheDir(t *testing.T) {
	if _, err := embedding.NewLocalEmbedder(embedding.LocalConfig{}); err == nil {
		t.Error("NewLocalEmbedder without a cache dir succeeded, want error")
	}
}

// requireLocalProvider skips unless the provider suite was explicitly requested.
func requireLocalProvider(t *testing.T) {
	t.Helper()

	if os.Getenv("DOCMCP_TEST_LOCAL") != "1" {
		t.Skip("set DOCMCP_TEST_LOCAL=1 to run the built-in embedder tests")
	}
}

// localCacheDir points the model tests at the shared machine cache.
//
// It deliberately does not use t.TempDir(): the model is ~190 MB, and giving
// each test its own would download it repeatedly. The directory is overridable
// so a sandboxed environment can place it somewhere writable.
func localCacheDir(t *testing.T) string {
	t.Helper()

	if dir := os.Getenv("DOCMCP_TEST_LOCAL_CACHE"); dir != "" {
		return dir
	}

	dir := embedding.DefaultCacheDir()
	if dir == "" {
		t.Skip("no writable cache directory for the local model")
	}
	return dir
}

func sameVector(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cosine(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
