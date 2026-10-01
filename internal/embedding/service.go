package embedding

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrDimensionMismatch reports a provider that returned vectors of the
	// wrong width, or the wrong number of them. Writing those into an index
	// alongside existing vectors would silently corrupt retrieval.
	ErrDimensionMismatch = errors.New("embedding dimension mismatch")
)

// Embedder turns text into vectors. The interface lives here so callers depend
// on the contract, not on Ollama, OpenAI, or a local model.
type Embedder interface {
	// Embed returns one vector per input text, in the same order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	Provider() string
	Model() string
	Dimensions() int
}

// Service batches calls to an Embedder and enforces the vector contract, so
// every provider is held to the same guarantees.
type Service struct {
	embedder Embedder
	batch    int
}

const defaultBatchSize = 32

type serviceOption func(*Service)

// WithBatchSize sets how many texts go to the provider in one call.
func WithBatchSize(n int) serviceOption {
	return func(s *Service) {
		if n > 0 {
			s.batch = n
		}
	}
}

func NewService(embedder Embedder, opts ...serviceOption) *Service {
	s := &Service{embedder: embedder, batch: defaultBatchSize}
	for _, apply := range opts {
		apply(s)
	}
	return s
}

// Provider, Model, and Dimensions expose the underlying embedder so a service
// can be handed to a caller that only needs the vector contract.
func (s *Service) Provider() string { return s.embedder.Provider() }
func (s *Service) Model() string    { return s.embedder.Model() }
func (s *Service) Dimensions() int  { return s.embedder.Dimensions() }

// Embed batches the texts and concatenates the results, preserving input order
// so a caller can pair each vector back to its chunk.
func (s *Service) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += s.batch {
		end := min(start+s.batch, len(texts))

		vectors, err := s.embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed %d of %d texts: %w", start, len(texts), err)
		}
		if err := s.validate(vectors, end-start); err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}

	return out, nil
}

// validate enforces that a provider returned exactly one correctly-sized vector
// per input text.
func (s *Service) validate(vectors [][]float32, want int) error {
	if len(vectors) != want {
		return fmt.Errorf("%w: provider returned %d vectors for %d texts",
			ErrDimensionMismatch, len(vectors), want)
	}

	wantDim := s.embedder.Dimensions()
	for i, v := range vectors {
		if len(v) != wantDim {
			return fmt.Errorf("%w: vector %d has %d dimensions, want %d from %s/%s",
				ErrDimensionMismatch, i, len(v), wantDim,
				s.embedder.Provider(), s.embedder.Model())
		}
	}
	return nil
}

// Identity is the persisted fingerprint of an index: provider, model, and
// dimensions. Changing any part means the index holds a different vector space.
func (s *Service) Identity() (string, error) {
	return fmt.Sprintf("%s/%s/%d",
		s.embedder.Provider(), s.embedder.Model(), s.embedder.Dimensions()), nil
}

// SameConfig reports whether two services would write vectors into the same
// space. It is the check behind the "one embedding model per index" rule.
func (s *Service) SameConfig(other *Service) bool {
	if other == nil {
		return false
	}
	if s.embedder.Provider() != other.embedder.Provider() ||
		s.embedder.Model() != other.embedder.Model() ||
		s.embedder.Dimensions() != other.embedder.Dimensions() {
		return false
	}
	return true
}

// IdentityMismatchError explains that the configured embedder no longer matches
// the index, and names the command that resolves it.
type IdentityMismatchError struct {
	Existing   string
	Configured string
}

func (e *IdentityMismatchError) Error() string {
	return fmt.Sprintf(
		"embedding configuration changed.\n\n"+
			"Existing index:\n  %s\n\n"+
			"Configured:\n  %s\n\n"+
			"Run:\n  docmcp reindex",
		e.Existing, e.Configured)
}

// CheckIdentity returns an IdentityMismatchError when the configured embedder
// differs from the one that wrote the index.
func (s *Service) CheckIdentity(existing string) error {
	if strings.TrimSpace(existing) == "" {
		return nil
	}

	configured, err := s.Identity()
	if err != nil {
		return err
	}
	if configured == existing {
		return nil
	}
	return &IdentityMismatchError{Existing: existing, Configured: configured}
}
