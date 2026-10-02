package embedding

import (
	"fmt"
	"math"
)

// ValidateEmbedding checks that a vector is nonempty, has the expected width
// when known, and contains only finite values.
func ValidateEmbedding(vec []float32, expectedDimensions int) error {
	if len(vec) == 0 {
		return fmt.Errorf("%w: empty vector", ErrDimensionMismatch)
	}
	if expectedDimensions > 0 && len(vec) != expectedDimensions {
		return fmt.Errorf("%w: vector has %d dimensions, want %d", ErrDimensionMismatch, len(vec), expectedDimensions)
	}
	for i, value := range vec {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%w: vector value %d is not finite", ErrDimensionMismatch, i)
		}
	}
	return nil
}
