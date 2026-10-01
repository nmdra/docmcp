package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// APIKeyEnvVar is the only supported source for OpenAI-compatible credentials.
// A key must never reach source metadata or an index.
const APIKeyEnvVar = "DOCMCP_OPENAI_API_KEY"

const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// defaultOpenAIDimensions is the published width for the default model.
const defaultOpenAIDimensions = 1536

// OpenAIConfig points at any OpenAI-compatible embeddings endpoint.
type OpenAIConfig struct {
	BaseURL string
	Model   string
	APIKey  string
	Timeout time.Duration

	// Dimensions, when zero, defaults to the model's published width.
	Dimensions int
}

// OpenAIEmbedder speaks the OpenAI /v1/embeddings shape, which most compatible
// services also implement.
type OpenAIEmbedder struct {
	baseURL    string
	model      string
	apiKey     string
	dimensions int
	client     *http.Client
}

func NewOpenAIEmbedder(cfg OpenAIConfig) (*OpenAIEmbedder, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("openai embedder: model is required")
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(APIKeyEnvVar))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("openai embedder: no API key; set %s or pass one in config", APIKeyEnvVar)
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("openai embedder: bad base URL %q: %w", cfg.BaseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("openai embedder: base URL must be http or https, got %q", parsed.Scheme)
	}

	dimensions := cfg.Dimensions
	if dimensions <= 0 {
		dimensions = defaultOpenAIDimensions
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	return &OpenAIEmbedder{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		model:      cfg.Model,
		apiKey:     apiKey,
		dimensions: dimensions,
		client:     &http.Client{Timeout: timeout},
	}, nil
}

func (e *OpenAIEmbedder) Provider() string { return "openai" }
func (e *OpenAIEmbedder) Model() string    { return e.model }
func (e *OpenAIEmbedder) Dimensions() int  { return e.dimensions }

// SetBaseURL repoints the embedder. It exists so a test can swap in a local
// server without rebuilding the key.
func (e *OpenAIEmbedder) SetBaseURL(baseURL string) {
	e.baseURL = strings.TrimSuffix(baseURL, "/")
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": texts,
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedder: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embedder: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("openai embedder: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embedder: %s: %s", resp.Status, providerMessage(payload))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("openai embedder: decode response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai embedder: %w: got %d vectors for %d texts",
			ErrDimensionMismatch, len(parsed.Data), len(texts))
	}

	// The API may return rows out of order; the index field is the contract.
	ordered := make([][]float32, len(parsed.Data))
	for _, row := range parsed.Data {
		if row.Index < 0 || row.Index >= len(ordered) {
			return nil, fmt.Errorf("openai embedder: %w: response index %d out of range",
				ErrDimensionMismatch, row.Index)
		}
		ordered[row.Index] = row.Embedding
	}
	for i, v := range ordered {
		if v == nil {
			return nil, fmt.Errorf("openai embedder: %w: response is missing vector %d",
				ErrDimensionMismatch, i)
		}
	}

	return ordered, nil
}
