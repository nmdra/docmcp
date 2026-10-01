package embedding_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/embedding"
)

func TestOpenAIEmbedder_Endpoint(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeOpenAIEmbeddings(w, [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}})
	}))
	defer srv.Close()

	e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL + "/v1",
		Model:   "text-embedding-3-small",
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder: %v", err)
	}

	got, err := e.Embed(t.Context(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("returned %d vectors, want 2", len(got))
	}

	if gotPath != "/v1/embeddings" {
		t.Errorf("posted to %q, want /v1/embeddings", gotPath)
	}
}

func TestOpenAIEmbedder_AuthHeader(t *testing.T) {
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeOpenAIEmbeddings(w, [][]float32{{0.1}})
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "sk-secret",
	})

	if _, err := e.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-secret")
	}
}

func TestOpenAIEmbedder_Model(t *testing.T) {
	var gotModel string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		writeOpenAIEmbeddings(w, [][]float32{{0.1}})
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "text-embedding-3-large", APIKey: "k",
	})

	if _, err := e.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotModel != "text-embedding-3-large" {
		t.Errorf("model = %q, want text-embedding-3-large", gotModel)
	}
}

func TestOpenAIEmbedder_Batch(t *testing.T) {
	var gotInputs []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		gotInputs = req.Input

		vectors := make([][]float32, len(req.Input))
		for i := range vectors {
			vectors[i] = []float32{0.1}
		}
		writeOpenAIEmbeddings(w, vectors)
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})

	if _, err := e.Embed(t.Context(), []string{"alpha", "bravo", "charlie"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(gotInputs) != 3 || gotInputs[0] != "alpha" || gotInputs[2] != "charlie" {
		t.Errorf("inputs sent = %v, want all three in order in one request", gotInputs)
	}
}

func TestOpenAIEmbedder_ErrorPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit"}}`))
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})

	_, err := e.Embed(t.Context(), []string{"a"})
	if err == nil {
		t.Fatal("Embed succeeded against a rate-limited provider, want error")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestOpenAIEmbedder_MissingAPIKey(t *testing.T) {
	t.Setenv("DOCMCP_OPENAI_API_KEY", "")

	if _, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: "https://api.openai.test/v1",
		Model:   "m",
	}); err == nil {
		t.Error("NewOpenAIEmbedder without an API key succeeded, want error")
	}
}

func TestOpenAIEmbedder_ReadsAPIKeyFromEnvironment(t *testing.T) {
	t.Setenv("DOCMCP_OPENAI_API_KEY", "sk-from-env")

	e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: "https://api.openai.test/v1",
		Model:   "m",
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder: %v", err)
	}

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeOpenAIEmbeddings(w, [][]float32{{0.1}})
	}))
	defer srv.Close()

	e.SetBaseURL(srv.URL)

	if _, err := e.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer sk-from-env" {
		t.Errorf("Authorization = %q, want the environment key", gotAuth)
	}
}

func TestOpenAIEmbedder_Identity(t *testing.T) {
	e, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: "https://api.openai.test/v1", Model: "text-embedding-3-small", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder: %v", err)
	}

	if e.Provider() != "openai" {
		t.Errorf("Provider() = %q, want openai", e.Provider())
	}
	if e.Model() != "text-embedding-3-small" {
		t.Errorf("Model() = %q", e.Model())
	}
}

func TestOpenAIEmbedder_RejectsMissingModel(t *testing.T) {
	if _, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: "https://api.openai.test/v1", APIKey: "k",
	}); err == nil {
		t.Error("NewOpenAIEmbedder without a model succeeded, want error")
	}
}

func TestOpenAIEmbedder_RejectsShortResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// One vector for two inputs.
		w.Write([]byte(`{"data":[{"embedding":[1,2],"index":0}]}`))
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})

	if _, err := e.Embed(t.Context(), []string{"a", "b"}); err == nil {
		t.Error("Embed accepted a response with the wrong vector count, want error")
	}
}

func TestOpenAIEmbedder_ReordersByIndex(t *testing.T) {
	// A provider is allowed to answer out of order; the index field is the
	// contract, and pairing rows by arrival would silently misalign vectors
	// with their chunks.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			// vector value encodes the requested position
			data[len(req.Input)-1-i] = map[string]any{
				"embedding": []float32{float32(i)},
				"index":     i,
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})

	got, err := e.Embed(t.Context(), []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	for i, v := range got {
		if v[0] != float32(i) {
			t.Errorf("vector %d holds value %v, want %d: rows were paired by arrival", i, v[0], i)
		}
	}
}

func TestOpenAIEmbedder_DoesNotLeakKeyInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	e, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "sk-super-secret",
	})

	_, err := e.Embed(t.Context(), []string{"a"})
	if err == nil {
		t.Fatal("Embed succeeded with an unauthorized key, want error")
	}
	if strings.Contains(err.Error(), "sk-super-secret") {
		t.Errorf("error leaked the API key: %v", err)
	}
}

func writeOpenAIEmbeddings(w http.ResponseWriter, vectors [][]float32) {
	data := make([]map[string]any, len(vectors))
	for i, v := range vectors {
		data[i] = map[string]any{"embedding": v, "index": i}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"model":  "m",
		"data":   data,
	})
}
