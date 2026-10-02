package embedding_test

import (
	"errors"
	"math"
	"testing"

	"github.com/docmcp/docmcp/internal/embedding"
)

func TestEmbeddingValidator_Empty(t *testing.T) {
	for _, vector := range [][]float32{nil, {}} {
		if err := embedding.ValidateEmbedding(vector, 0); !errors.Is(err, embedding.ErrDimensionMismatch) {
			t.Fatalf("ValidateEmbedding(%v, 0) = %v, want ErrDimensionMismatch", vector, err)
		}
	}
}

func TestEmbeddingValidator_DimensionMismatch(t *testing.T) {
	if err := embedding.ValidateEmbedding([]float32{1, 2}, 3); !errors.Is(err, embedding.ErrDimensionMismatch) {
		t.Fatalf("error = %v, want ErrDimensionMismatch", err)
	}
}

func TestEmbeddingValidator_NaN(t *testing.T) {
	if err := embedding.ValidateEmbedding([]float32{1, float32(math.NaN())}, 2); !errors.Is(err, embedding.ErrDimensionMismatch) {
		t.Fatalf("error = %v, want ErrDimensionMismatch", err)
	}
}

func TestEmbeddingValidator_Infinity(t *testing.T) {
	for _, sign := range []int{-1, 1} {
		if err := embedding.ValidateEmbedding([]float32{float32(math.Inf(sign)), 1}, 2); !errors.Is(err, embedding.ErrDimensionMismatch) {
			t.Fatalf("error = %v, want ErrDimensionMismatch", err)
		}
	}
}

func TestEmbeddingValidator_Valid(t *testing.T) {
	for _, dimensions := range []int{0, -1, 3} {
		vector := []float32{0, -1, math.MaxFloat32}
		if err := embedding.ValidateEmbedding(vector, dimensions); err != nil {
			t.Fatalf("ValidateEmbedding(valid, %d): %v", dimensions, err)
		}
	}
}
