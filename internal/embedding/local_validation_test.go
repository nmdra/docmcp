package embedding

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// fixedLocalFunction substitutes the ONNX dependency without loading a model.
type fixedLocalFunction struct {
	embeddings.EmbeddingFunction
	vector []float32
}

func (f *fixedLocalFunction) EmbedQuery(context.Context, string) (embeddings.Embedding, error) {
	return embeddings.NewEmbeddingFromFloat32(f.vector), nil
}

func TestLocalEmbedder_RejectsNonfiniteProviderOutput(t *testing.T) {
	for _, value := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		provider := &LocalEmbedder{fn: &fixedLocalFunction{vector: []float32{1, value}}, dim: 2}
		got, err := provider.Embed(t.Context(), []string{"text"})
		if !errors.Is(err, ErrDimensionMismatch) || got != nil {
			t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", got, err)
		}
	}
}

func TestLocalEmbedder_ValidOutputIsPreserved(t *testing.T) {
	vector := []float32{0, -1, 2}
	provider := &LocalEmbedder{fn: &fixedLocalFunction{vector: vector}, dim: 3}
	got, err := provider.Embed(t.Context(), []string{"text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0]) != 3 {
		t.Fatalf("vectors = %v", got)
	}
	for i, want := range vector {
		if got[0][i] != want {
			t.Fatalf("value %d = %v, want %v", i, got[0][i], want)
		}
	}
	if provider.Dimensions() != 3 {
		t.Fatalf("dimensions = %d, want 3", provider.Dimensions())
	}
}
