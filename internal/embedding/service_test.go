package embedding_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/docmcp/docmcp/internal/embedding"
)

// FakeEmbedder is a hand-written stand-in for a real provider. It records every
// call so tests can prove batching and, crucially, that sync did *not* re-embed
// unchanged text.
type FakeEmbedder struct {
	mu      sync.Mutex
	Calls   [][]string
	Batch   int
	Dim     int
	Err     error
	failsAt int
}

func NewFakeEmbedder(dim int) *FakeEmbedder {
	if dim <= 0 {
		dim = 4
	}
	return &FakeEmbedder{Dim: dim}
}

func (f *FakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	call := append([]string(nil), texts...)
	f.Calls = append(f.Calls, call)
	if f.Err != nil {
		err := f.Err
		f.mu.Unlock()
		return nil, err
	}
	failsAt := f.failsAt
	f.mu.Unlock()

	if failsAt > 0 && len(f.Calls) == failsAt {
		return nil, errors.New("fake embedder: injected failure")
	}

	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = deterministicVector(t, f.Dim)
	}
	return out, ctx.Err()
}

func (f *FakeEmbedder) Name() string     { return "fake" }
func (f *FakeEmbedder) Dimensions() int  { return f.Dim }
func (f *FakeEmbedder) Model() string    { return "fake-model" }
func (f *FakeEmbedder) Provider() string { return "fake" }

// EmbeddedText returns every text the embedder was asked to embed, in order.
func (f *FakeEmbedder) EmbeddedText() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string
	for _, call := range f.Calls {
		out = append(out, call...)
	}
	return out
}

func (f *FakeEmbedder) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// deterministicVector gives identical text an identical vector, which is the
// only property retrieval correctness actually depends on.
func deterministicVector(text string, dim int) []float32 {
	v := make([]float32, dim)
	for i, r := range text {
		v[i%dim] += float32(r%13) / 13
	}
	return v
}

func TestEmbeddingService_BatchesChunks(t *testing.T) {
	fake := NewFakeEmbedder(8)

	svc := embedding.NewService(fake, embedding.WithBatchSize(2))

	var texts []string
	for i := range 7 {
		texts = append(texts, "chunk number "+strings.Repeat("x", i))
	}

	got, err := svc.Embed(t.Context(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(texts) {
		t.Fatalf("returned %d vectors, want %d", len(got), len(texts))
	}

	if fake.CallCount() != 4 {
		t.Errorf("embedder called %d times, want 4 batches of 2 for 7 texts", fake.CallCount())
	}
}

func TestEmbeddingService_BatchSizeOneCallsPerText(t *testing.T) {
	fake := NewFakeEmbedder(4)
	svc := embedding.NewService(fake, embedding.WithBatchSize(1))

	if _, err := svc.Embed(t.Context(), []string{"a", "b", "c"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if fake.CallCount() != 3 {
		t.Errorf("embedder called %d times, want 3", fake.CallCount())
	}
}

func TestEmbeddingService_PreservesOrder(t *testing.T) {
	fake := NewFakeEmbedder(6)
	svc := embedding.NewService(fake, embedding.WithBatchSize(2))

	texts := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	got, err := svc.Embed(t.Context(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	for i := range texts {
		want := deterministicVector(texts[i], 6)
		for j := range want {
			if got[i][j] != want[j] {
				t.Fatalf("vector %d does not match its input %q: batching reordered results", i, texts[i])
			}
		}
	}
}

func TestEmbeddingService_EmptyInputMakesNoCall(t *testing.T) {
	fake := NewFakeEmbedder(4)
	svc := embedding.NewService(fake)

	got, err := svc.Embed(t.Context(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("returned %d vectors for no input, want 0", len(got))
	}
	if fake.CallCount() != 0 {
		t.Errorf("embedder called %d times for no input, want 0", fake.CallCount())
	}
}

func TestEmbeddingService_ReturnsDimensionMismatchError(t *testing.T) {
	// A provider that returns wrong-width vectors must be caught even when it
	// reports success — that is the case that would corrupt an index.
	wrongWidth := &wrongDimensionEmbedder{}

	svc := embedding.NewService(wrongWidth)

	_, err := svc.Embed(t.Context(), []string{"a", "b"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) {
		t.Errorf("error = %v, want ErrDimensionMismatch", err)
	}
}

func TestEmbeddingService_PropagatesProviderError(t *testing.T) {
	fake := NewFakeEmbedder(4)
	fake.Err = errors.New("connection refused")

	svc := embedding.NewService(fake)

	if _, err := svc.Embed(t.Context(), []string{"a"}); err == nil {
		t.Error("Embed succeeded despite a provider error, want error")
	}
}

func TestEmbeddingService_RejectsWrongVectorCount(t *testing.T) {
	fake := &shortEmbedder{}

	svc := embedding.NewService(fake)

	_, err := svc.Embed(t.Context(), []string{"a", "b"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) {
		t.Errorf("error = %v, want ErrDimensionMismatch for a short vector list", err)
	}
}

func TestEmbeddingService_IdentityIsStable(t *testing.T) {
	fake := NewFakeEmbedder(384)

	svc := embedding.NewService(fake)

	first, err := svc.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}

	want := "fake/fake-model/384"
	if first != want {
		t.Errorf("Identity() = %q, want %q", first, want)
	}

	other := embedding.NewService(NewFakeEmbedder(384))
	second, err := other.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if first != second {
		t.Errorf("Identity differs for the same provider/model: %q vs %q", first, second)
	}
}

func TestEmbeddingService_MismatchDetectedBetweenConfigs(t *testing.T) {
	small := embedding.NewService(NewFakeEmbedder(384))
	large := embedding.NewService(NewFakeEmbedder(768))

	if small.SameConfig(large) {
		t.Error("SameConfig() = true for different dimensions, want false")
	}

	same := embedding.NewService(NewFakeEmbedder(384))
	if !small.SameConfig(same) {
		t.Error("SameConfig() = false for identical configuration, want true")
	}
}

// wrongDimensionEmbedder reports success but returns 8-wide vectors while
// claiming 4 dimensions.
type wrongDimensionEmbedder struct{}

func (w *wrongDimensionEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, 8)
	}
	return out, nil
}
func (w *wrongDimensionEmbedder) Name() string     { return "wrong" }
func (w *wrongDimensionEmbedder) Model() string    { return "wrong-model" }
func (w *wrongDimensionEmbedder) Provider() string { return "fake" }
func (w *wrongDimensionEmbedder) Dimensions() int  { return 4 }

// shortEmbedder returns fewer vectors than it was given, the way a misbehaving
// provider would.
type shortEmbedder struct{}

func (s *shortEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return make([][]float32, len(texts)-1), nil
}
func (s *shortEmbedder) Name() string     { return "short" }
func (s *shortEmbedder) Model() string    { return "short-model" }
func (s *shortEmbedder) Provider() string { return "fake" }
func (s *shortEmbedder) Dimensions() int  { return 4 }
