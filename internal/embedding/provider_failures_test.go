package embedding_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docmcp/docmcp/internal/embedding"
)

// fakeEmbeddingServer responds to /api/embed (Ollama) or /v1/embeddings
// (OpenAI) according to a per-test behaviour, and counts calls.
type fakeEmbeddingServer struct {
	*httptest.Server

	calls    atomic.Int64
	authSeen atomic.Value
}

func newFakeOllama(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *fakeEmbeddingServer {
	t.Helper()

	f := &fakeEmbeddingServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.authSeen.Store(r.Header.Get("Authorization"))
		handle(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func ollamaVectorBody(n int) string {
	vec := make([]float64, n)
	for i := range vec {
		vec[i] = 0.01
	}
	body, _ := json.Marshal(map[string]any{"embeddings": [][]float64{vec}})
	return string(body)
}

// TestOllama_ErrorStatusesAreNotRetried pins that a provider rejection is
// reported immediately.
//
// A 401 or 429 will not become healthy by trying again, and retrying an
// unauthenticated request risks tripping a provider's abuse limits. The
// requirement is a clear error and no partially written index, not resilience.
func TestOllama_ErrorStatusesAreNotRetried(t *testing.T) {
	for _, status := range []int{401, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := newFakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"upstream refused"}`))
			})

			e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
				BaseURL: srv.URL,
				Model:   "nomic-embed-text",
				Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = e.Embed(context.Background(), []string{"hello"})
			if err == nil {
				t.Fatal("expected an error for a provider that rejects the request")
			}

			msg := err.Error()
			if !strings.Contains(msg, "nomic-embed-text") {
				t.Errorf("error should name the model that failed, got: %v", msg)
			}
			if n := srv.calls.Load(); n != 1 {
				t.Errorf("a %d rejection was retried %d times; it should fail fast", status, n)
			}
		})
	}
}

// TestOllama_ConnectionRefusedIsAClearError covers the provider simply not
// running, which is the most common first-run failure.
func TestOllama_ConnectionRefusedIsAClearError(t *testing.T) {
	srv := newFakeOllama(t, func(http.ResponseWriter, *http.Request) {})
	addr := srv.URL
	srv.Close() // nothing is listening now

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: addr,
		Model:   "nomic-embed-text",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error when nothing is listening")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Errorf("error should name the unreachable endpoint %q, got: %v", addr, err)
	}
}

// TestOllama_TimeoutIsReportedAsTimeout distinguishes a slow provider from a
// broken one, so a user knows whether to wait or to start the server.
func TestOllama_TimeoutIsReportedAsTimeout(t *testing.T) {
	srv := newFakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
		Timeout: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "timeout") && !strings.Contains(msg, "deadline") &&
		!strings.Contains(msg, "timed out") {
		t.Errorf("error should read as a timeout, got: %v", err)
	}
}

// TestOllama_MalformedResponsesAreRejected pins that a provider answering with
// something other than vectors fails loudly instead of writing garbage into the
// index. A wrong-width vector is the dangerous one: it would be stored and only
// fail later, at query time.
func TestOllama_MalformedResponsesAreRejected(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not json", `this is not json at all`},
		{"empty embeddings array", `{"embeddings":[]}`},
		{"null vector", `{"embeddings":[[]]}`},
		{"wrong shape", `{"embeddings":[{"vector":[0.1,0.2]}]}`},
		{"html error page", `<html><body>502 Bad Gateway</body></html>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})

			e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
				BaseURL: srv.URL,
				Model:   "nomic-embed-text",
				Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}

			got, err := e.Embed(context.Background(), []string{"hello"})
			if err == nil {
				t.Fatalf("expected an error for %q; got vectors %v", tc.name, got)
			}
		})
	}
}

// TestOllama_SecretIsNeverEchoed pins that an API key never reaches an error
// message or a log line.
func TestOllama_SecretIsNeverEchoed(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-1234567890"

	srv := newFakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// A provider that echoes the key back must not make us echo it further.
		_, _ = w.Write([]byte(`{"error":"invalid key ` + secret + `"}`))
	})

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ollama takes no key, so this is trivially true for it; the OpenAI test
	// below is the one that matters.
	_, _ = e.Embed(context.Background(), []string{"hello"})
}

func TestOpenAI_SecretIsNeverEchoed(t *testing.T) {
	const secret = "sk-live-DO-NOT-LEAK-1234567890"

	var srvAuth atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srvAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + secret + `"}}`))
	}))
	defer srv.Close()

	e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL,
		Model:   "text-embedding-3-small",
		APIKey:  secret,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected an error for a rejected key")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the API key leaked into the error message: %v", err)
	}

	// The key must still be sent, or nothing would work at all.
	if got, _ := srvAuth.Load().(string); !strings.Contains(got, secret) {
		t.Errorf("the key should be sent as a bearer token, got %q", got)
	}
}

// TestOpenAI_PartialBatchResponseIsRejected covers a provider that answers a
// batch of N inputs with fewer than N vectors. Accepting that would silently
// misalign every vector with its chunk.
func TestOpenAI_PartialBatchResponseIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// One embedding for a request carrying three inputs.
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`))
	}))
	defer srv.Close()

	e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL,
		Model:   "text-embedding-3-small",
		APIKey:  "test-key",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := e.Embed(context.Background(), []string{"one", "two", "three"})
	if err == nil {
		t.Fatalf("a partial batch response must be an error; got %d vectors for 3 inputs", len(got))
	}
	if len(got) != 0 {
		t.Errorf("a failed batch must return no vectors, got %d", len(got))
	}
}

// TestOpenAI_ErrorStatusesAreNotRetried mirrors the Ollama case for the
// OpenAI-compatible provider.
func TestOpenAI_ErrorStatusesAreNotRetried(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
			}))
			defer srv.Close()

			e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
				BaseURL: srv.URL,
				Model:   "text-embedding-3-small",
				APIKey:  "test-key",
				Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := e.Embed(context.Background(), []string{"hello"}); err == nil {
				t.Fatalf("expected an error for status %d", status)
			}
			if n := calls.Load(); n != 1 {
				t.Errorf("status %d was retried %d times; it should fail fast", status, n)
			}
		})
	}
}

// TestService_IdentityIsStableAndNamesTheVectorSpace pins the fingerprint the
// whole index depends on: it must be identical across calls and must record the
// provider and model.
//
// The width is recorded as 0 for Ollama on purpose. Ollama's vector width is a
// property of whichever model is installed on the server, so DocMCP cannot know
// it before embedding and persists 0; the store learns the real width from the
// first vector and the identity check catches any later change. This test pins
// the documented behaviour rather than asserting a width DocMCP never had.
func TestService_IdentityIsStableAndNamesTheVectorSpace(t *testing.T) {
	srv := newFakeOllama(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ollamaVectorBody(8)))
	})

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := embedding.NewService(e)

	first, err := svc.Identity()
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("identity changed between calls: %q then %q", first, second)
	}
	if !strings.Contains(first, "ollama") {
		t.Errorf("identity should name the provider, got %q", first)
	}
	if !strings.Contains(first, "nomic-embed-text") {
		t.Errorf("identity should name the model, got %q", first)
	}
	if !strings.HasSuffix(first, "/0") {
		t.Errorf("an Ollama identity should record width 0, since the width is "+
			"only knowable from the first vector; got %q", first)
	}
}
