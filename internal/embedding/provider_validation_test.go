package embedding_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docmcp/docmcp/internal/embedding"
)

func TestOpenAI_RejectsWrongWidthAtProviderBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,2],"index":0}]}`))
	}))
	defer server.Close()
	provider, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{BaseURL: server.URL, Model: "test", APIKey: "test", Dimensions: 3})
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := provider.Embed(t.Context(), []string{"text"})
	if !errors.Is(err, embedding.ErrDimensionMismatch) || vectors != nil {
		t.Fatalf("Embed = %v, %v, want nil and ErrDimensionMismatch", vectors, err)
	}
}

func TestOllama_ServiceAcceptsFiniteResponseWithUnknownDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embedding":[0,-1,2]}`))
	}))
	defer server.Close()
	provider, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: server.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	service := embedding.NewService(provider)
	vectors, err := service.Embed(t.Context(), []string{"text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 3 || vectors[0][1] != -1 {
		t.Fatalf("vectors = %v", vectors)
	}
	identity, err := service.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if provider.Dimensions() != 0 || identity != "ollama/test/0" {
		t.Fatalf("identity changed: %q", identity)
	}
}

// JSON cannot encode NaN or infinity. Both invalid literals and float32
// overflow must fail at the public HTTP provider boundary without any vectors.
func TestHTTPProviders_RejectNonfiniteResponse(t *testing.T) {
	for _, value := range []string{"NaN", "Infinity", "-Infinity", "1e39", "-1e39"} {
		for _, name := range []string{"ollama", "openai"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					body := `{"embedding":[` + value + `]}`
					if name == "openai" {
						body = `{"data":[{"embedding":[` + value + `],"index":0}]}`
					}
					_, _ = w.Write([]byte(body))
				}))
				defer server.Close()
				var provider embedding.Embedder
				var err error
				if name == "ollama" {
					provider, err = embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: server.URL, Model: "test"})
				} else {
					provider, err = embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{BaseURL: server.URL, Model: "test", APIKey: "test", Dimensions: 1})
				}
				if err != nil {
					t.Fatal(err)
				}
				vectors, err := provider.Embed(t.Context(), []string{"text"})
				if err == nil || vectors != nil {
					t.Fatalf("Embed = %v, %v, want nil vectors and error", vectors, err)
				}
			})
		}
	}
}
