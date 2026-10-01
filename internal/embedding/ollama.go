package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultOllamaBaseURL = "http://localhost:11434"

// OllamaConfig points at a local Ollama server. Ollama needs no API key, so
// none is accepted here.
type OllamaConfig struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

// OllamaEmbedder talks to Ollama's HTTP API. It sends one text per request
// because that endpoint takes a single prompt.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaEmbedder(cfg OllamaConfig) (*OllamaEmbedder, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("ollama embedder: model is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: bad base URL %q: %w", cfg.BaseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("ollama embedder: base URL must be http or https, got %q", parsed.Scheme)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return &OllamaEmbedder{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   cfg.Model,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (e *OllamaEmbedder) Provider() string { return "ollama" }
func (e *OllamaEmbedder) Model() string    { return e.model }
func (e *OllamaEmbedder) BaseURL() string  { return e.baseURL }

// Dimensions is not declared in config: Ollama's width depends on the model, so
// the store learns it from the first vector and the identity check catches any
// later change.
func (e *OllamaEmbedder) Dimensions() int { return 0 }

func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))

	for _, text := range texts {
		vector, err := e.embedOne(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vector)
	}

	return out, nil
}

func (e *OllamaEmbedder) embedOne(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model":  e.model,
		"prompt": text,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("ollama embedder: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embedder: %s from %s for model %s: %s",
			resp.Status, e.baseURL, e.model, providerMessage(payload))
	}

	var parsed struct {
		Embedding  []float32   `json:"embedding"`
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embedder: decode response: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama embedder: %s", parsed.Error)
	}

	switch {
	case len(parsed.Embedding) > 0:
		return parsed.Embedding, nil
	case len(parsed.Embeddings) > 0 && len(parsed.Embeddings[0]) > 0:
		return parsed.Embeddings[0], nil
	default:
		// A zero-length vector is not a vector. Accepting one would write an
		// unsearchable chunk into the index and only fail later, at query time.
		return nil, fmt.Errorf("ollama embedder: %w: response from %s carried no vector for model %s",
			ErrDimensionMismatch, e.baseURL, e.model)
	}
}

// providerMessage pulls the human-readable part out of an error payload,
// falling back to the raw body.
func providerMessage(payload []byte) string {
	var parsed struct {
		Error  string `json:"error"`
		Detail any    `json:"detail"`
	}
	if err := json.Unmarshal(payload, &parsed); err == nil {
		if parsed.Error != "" {
			return parsed.Error
		}
		if parsed.Detail != nil {
			if s, ok := parsed.Detail.(string); ok && s != "" {
				return s
			}
		}
	}

	if trimmed := strings.TrimSpace(string(payload)); trimmed != "" {
		return truncate(trimmed, 300)
	}
	return "no details provided"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
