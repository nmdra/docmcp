package embedding_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/nmdra/docmcp/internal/embedding"
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

// responseEmbedder returns provider output unchanged to exercise the service boundary.
type responseEmbedder struct {
	vectors    [][]float32
	dimensions int
	calls      int
}

func (e *responseEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	return e.vectors, nil
}
func (e *responseEmbedder) Provider() string { return "fake" }
func (e *responseEmbedder) Model() string    { return "response" }
func (e *responseEmbedder) Dimensions() int  { return e.dimensions }

func TestEmbeddingService_RejectsNonfiniteVectorsBeforeReturningResults(t *testing.T) {
	for _, value := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
		provider := &responseEmbedder{vectors: [][]float32{{1, value}}, dimensions: 2}
		service := embedding.NewService(provider, embedding.WithBatchSize(1))
		got, err := service.Embed(t.Context(), []string{"invalid", "never requested"})
		if !errors.Is(err, embedding.ErrDimensionMismatch) {
			t.Fatalf("error = %v, want ErrDimensionMismatch", err)
		}
		if got != nil {
			t.Fatalf("invalid vectors reached caller: %v", got)
		}
		if provider.calls != 1 {
			t.Fatalf("calls = %d, want 1", provider.calls)
		}
	}
}

func TestEmbeddingService_AcceptsUnknownDimensionsWithoutChangingIdentity(t *testing.T) {
	provider := &responseEmbedder{vectors: [][]float32{{0, -1, 2}}, dimensions: 0}
	service := embedding.NewService(provider)
	got, err := service.Embed(t.Context(), []string{"valid"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, provider.vectors) {
		t.Fatalf("vectors changed: %v", got)
	}
	identity, err := service.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if service.Dimensions() != 0 || identity != "fake/response/0" {
		t.Fatalf("dimensions/identity changed: %d, %s", service.Dimensions(), identity)
	}
}

func TestEmbeddingService_RejectsEmptyVectorWithUnknownDimensions(t *testing.T) {
	provider := &responseEmbedder{vectors: [][]float32{nil}, dimensions: 0}
	got, err := embedding.NewService(provider).Embed(t.Context(), []string{"text"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) || got != nil {
		t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", got, err)
	}
}

// laterInvalidEmbedder succeeds on the first batch, then returns invalid output.
type laterInvalidEmbedder struct{ responseEmbedder }

func (e *laterInvalidEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	if e.calls == 1 {
		return [][]float32{{1, 2}}, nil
	}
	return [][]float32{{1, float32(math.NaN())}}, nil
}

func TestEmbeddingService_InvalidLaterBatchReturnsNoPartialVectors(t *testing.T) {
	provider := &laterInvalidEmbedder{responseEmbedder: responseEmbedder{dimensions: 2}}
	service := embedding.NewService(provider, embedding.WithBatchSize(1))
	got, err := service.Embed(t.Context(), []string{"valid", "invalid", "never requested"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) || got != nil {
		t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", got, err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want 2", provider.calls)
	}
}

func TestEmbeddingService_UnknownDimensionsRejectsMixedWidthsInBatch(t *testing.T) {
	provider := &responseEmbedder{vectors: [][]float32{{1, 2}, {3, 4, 5}}, dimensions: 0}
	service := embedding.NewService(provider)
	got, err := service.Embed(t.Context(), []string{"first", "second"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) || got != nil {
		t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", got, err)
	}
	identity, err := service.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if service.Dimensions() != 0 || identity != "fake/response/0" {
		t.Fatalf("dimensions/identity changed: %d, %s", service.Dimensions(), identity)
	}
}

// changingWidthEmbedder supplies a different width on each successive batch.
type changingWidthEmbedder struct{ responseEmbedder }

func (e *changingWidthEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	if e.calls == 1 {
		return [][]float32{{1, 2}}, nil
	}
	return [][]float32{{3, 4, 5}}, nil
}

func TestEmbeddingService_UnknownDimensionsRejectsMixedWidthsAcrossBatches(t *testing.T) {
	provider := &changingWidthEmbedder{}
	service := embedding.NewService(provider, embedding.WithBatchSize(1))
	got, err := service.Embed(t.Context(), []string{"first", "second", "never requested"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) || got != nil {
		t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", got, err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want 2", provider.calls)
	}
	identity, err := service.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if service.Dimensions() != 0 || identity != "fake/response/0" {
		t.Fatalf("dimensions/identity changed: %d, %s", service.Dimensions(), identity)
	}
}

func TestEmbeddingService_UnknownDimensionsInferenceIsLocalToEachCall(t *testing.T) {
	provider := &responseEmbedder{dimensions: 0}
	service := embedding.NewService(provider)
	for _, vector := range [][]float32{{1, 2}, {3, 4, 5}} {
		provider.vectors = [][]float32{vector}
		got, err := service.Embed(t.Context(), []string{"text"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, provider.vectors) {
			t.Fatalf("vectors changed: %v", got)
		}
	}
	identity, err := service.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if service.Dimensions() != 0 || identity != "fake/response/0" {
		t.Fatalf("dimensions/identity changed: %d, %s", service.Dimensions(), identity)
	}
}
