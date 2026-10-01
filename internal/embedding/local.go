package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	"github.com/amikos-tech/chroma-go/pkg/embeddings/ort"
)

// DefaultEmbeddingModel is the built-in model. It needs no API key and no
// running server, which is the whole reason it is offered.
const DefaultEmbeddingModel = "all-MiniLM-L6-v2"

// defaultDimensions is the published width for that model. It is persisted with
// the index, so a wrong value would be worse than no value.
const defaultDimensions = 384

// LocalConfig configures the built-in embedder.
type LocalConfig struct {
	// CacheDir is where the ONNX runtime and model are downloaded and kept. It is
	// required: the model is large enough that a guessed location is unhelpful.
	// Use DefaultCacheDir unless you have a reason not to.
	CacheDir string

	Timeout time.Duration
}

// DefaultCacheDir is the shared, per-machine location for the embedder's model.
//
// The model is roughly 190 MB and is identical for every index, so it belongs in
// a machine-level cache rather than beside any one index. Keeping it under a
// data directory would re-download the model for every --data-dir, and because
// the runtime loads it once per process, a second data dir would silently reuse
// the first one's copy.
func DefaultCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "docmcp", "models")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "docmcp", "models")
	}
	return ""
}

// LocalEmbedder runs the built-in sentence-transformer in-process through ONNX.
//
// The ONNX function is wrapped rather than reimplemented so DocMCP keeps
// ownership of the vector contract — Provider, Model, Dimensions — while the
// runtime stays an implementation detail behind the Embedder interface.
type LocalEmbedder struct {
	fn  embeddings.EmbeddingFunction
	dim int
}

// The ONNX runtime is a process-wide singleton with its own environment handle.
// It is created once and deliberately never torn down: the release function
// destroys shared state, so calling it while any embedder is still reachable
// makes the next Embed fail with "embedding function is closed". A CLI process
// that exits releases it anyway, and a long-lived server wants it alive for its
// whole lifetime.
var (
	localOnce    sync.Once
	localFn      embeddings.EmbeddingFunction
	localLoadErr error
)

func NewLocalEmbedder(cfg LocalConfig) (*LocalEmbedder, error) {
	cacheDir := strings.TrimSpace(cfg.CacheDir)
	if cacheDir == "" {
		return nil, fmt.Errorf("local embedder: cache directory is required")
	}

	absolute, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("local embedder: resolve cache directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("local embedder: create cache directory: %w", err)
	}

	// The ONNX helper derives every path from the home directory and reads its
	// environment once, on first use. Pointing HOME at the cache directory for the
	// duration of construction keeps the model beside the data instead of in a
	// user's home directory, and restores it immediately afterwards.
	restoreHome, err := withHomeDir(absolute)
	if err != nil {
		return nil, err
	}

	// The library reads its environment once, so HOME only has to point at the
	// cache for the first load; later callers reuse the loaded model.
	localOnce.Do(func() {
		var release func() error
		localFn, release, localLoadErr = ort.NewDefaultEmbeddingFunction()
		if release != nil {
			// Intentionally dropped: see the note on localOnce above.
			_ = release
		}
	})
	restoreHome()

	if localLoadErr != nil {
		return nil, fmt.Errorf(
			"local embedder: load %s into %s: %w "+
				"(the first run downloads the model and needs network access)",
			DefaultEmbeddingModel, absolute, localLoadErr)
	}
	if localFn == nil {
		return nil, fmt.Errorf("local embedder: %s is unavailable", DefaultEmbeddingModel)
	}
	// The release function destroys the process-wide ONNX environment, so it must
	// not be called while the embedder is still usable. It is registered with the
	// runtime teardown instead: an embedder outlives any single command.
	//
	// Tearing it down early produced "embedding function is closed" on the first
	// Embed call, which is exactly the failure this comment exists to prevent.
	return &LocalEmbedder{fn: localFn, dim: defaultDimensions}, nil
}

func (e *LocalEmbedder) Provider() string { return "default" }
func (e *LocalEmbedder) Model() string    { return DefaultEmbeddingModel }

// Dimensions is the vector width. It is fixed for this model, which is what
// makes it safe to persist with the index and compare on the next run.
func (e *LocalEmbedder) Dimensions() int { return e.dim }

func (e *LocalEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vector, err := e.embedOne(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vector)
	}
	return out, nil
}

func (e *LocalEmbedder) embedOne(ctx context.Context, text string) ([]float32, error) {
	if e.fn == nil {
		return nil, fmt.Errorf("local embedder: not initialized")
	}

	vector, err := e.fn.EmbedQuery(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("local embedder: embed: %w", err)
	}

	values := vector.ContentAsFloat32()
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: local embedder returned an empty vector", ErrDimensionMismatch)
	}

	if len(values) != e.dim {
		// A width change means the index's recorded dimensions no longer hold.
		return nil, fmt.Errorf("%w: local embedder produced %d dimensions, index expects %d",
			ErrDimensionMismatch, len(values), e.dim)
	}

	return values, nil
}

// withHomeDir points HOME at dir and returns a function restoring the previous
// value. It is used only around the ONNX library's one-time environment read.
func withHomeDir(dir string) (restore func(), err error) {
	previous, had := os.LookupEnv("HOME")

	if err := os.Setenv("HOME", dir); err != nil {
		return nil, fmt.Errorf("local embedder: set cache home: %w", err)
	}

	return func() {
		if had {
			os.Setenv("HOME", previous)
			return
		}
		os.Unsetenv("HOME")
	}, nil
}
