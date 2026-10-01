package embedding_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docmcp/docmcp/internal/embedding"
)

func TestOllamaEmbedder_Endpoint(t *testing.T) {
	var gotPath, gotModel string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model

		writeOllamaEmbeddings(w, [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}})
	}))
	defer srv.Close()

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder: %v", err)
	}

	got, err := e.Embed(t.Context(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("returned %d vectors, want 2", len(got))
	}

	if gotPath != "/api/embeddings" {
		t.Errorf("posted to %q, want /api/embeddings", gotPath)
	}
	if gotModel != "nomic-embed-text" {
		t.Errorf("model = %q, want nomic-embed-text", gotModel)
	}
}

func TestOllamaEmbedder_Batch(t *testing.T) {
	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req.Prompt)

		vec := make([]float32, 3)
		writeOllamaEmbeddings(w, [][]float32{vec})
	}))
	defer srv.Close()

	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder: %v", err)
	}

	for _, text := range []string{"alpha", "bravo", "charlie"} {
		if _, err := e.Embed(t.Context(), []string{text}); err != nil {
			t.Fatalf("Embed(%q): %v", text, err)
		}
	}

	if len(seen) != 3 || seen[0] != "alpha" || seen[2] != "charlie" {
		t.Errorf("prompts sent = %v, want alpha, bravo, charlie in order", seen)
	}
}

func TestOllamaEmbedder_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()

	e, _ := embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: srv.URL, Model: "missing"})

	_, err := e.Embed(t.Context(), []string{"a"})
	if err == nil {
		t.Fatal("Embed succeeded against a failing provider, want error")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error = %v, want it to carry the provider message", err)
	}
}

func TestOllamaEmbedder_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"embedding": "not-a-vector"}`))
	}))
	defer srv.Close()

	e, _ := embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: srv.URL, Model: "m"})

	if _, err := e.Embed(t.Context(), []string{"a"}); err == nil {
		t.Error("Embed accepted a malformed vector, want error")
	}
}

func TestOllamaEmbedder_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	e, _ := embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: srv.URL, Model: "m"})

	if _, err := e.Embed(t.Context(), []string{"a"}); err == nil {
		t.Error("Embed succeeded against a failing gateway, want error")
	}
}

func TestOllamaEmbedder_Identity(t *testing.T) {
	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder: %v", err)
	}

	if e.Provider() != "ollama" {
		t.Errorf("Provider() = %q, want ollama", e.Provider())
	}
	if e.Model() != "nomic-embed-text" {
		t.Errorf("Model() = %q", e.Model())
	}
}

func TestOllamaEmbedder_RejectsMissingBaseURL(t *testing.T) {
	if _, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{Model: "m"}); err == nil {
		t.Error("NewOllamaEmbedder without a base URL succeeded, want error")
	}
}

func TestOllamaEmbedder_DefaultsToLocalhost(t *testing.T) {
	e, err := embedding.NewOllamaEmbedder(embedding.OllamaConfig{Model: "m"})
	if err != nil {
		t.Fatalf("NewOllamaEmbedder: %v", err)
	}
	if e.BaseURL() != "http://localhost:11434" {
		t.Errorf("BaseURL() = %q, want the localhost default", e.BaseURL())
	}
}

func TestOllamaEmbedder_DoesNotSendAPIKey(t *testing.T) {
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeOllamaEmbeddings(w, [][]float32{{0.1, 0.2}})
	}))
	defer srv.Close()

	e, _ := embedding.NewOllamaEmbedder(embedding.OllamaConfig{BaseURL: srv.URL, Model: "m"})

	if _, err := e.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Ollama request carried an Authorization header %q, want none", gotAuth)
	}
}

func writeOllamaEmbeddings(w http.ResponseWriter, vectors [][]float32) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"embedding": vectors[0], "embeddings": vectors})
}
