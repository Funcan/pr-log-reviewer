package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Default endpoints for OpenAI-compatible backends.
const (
	GitHubModelsBaseURL = "https://models.github.ai/inference"
	OllamaBaseURL       = "http://localhost:11434/v1"
)

// OpenAICompatible talks to any backend that implements the OpenAI
// /chat/completions API: GitHub Models (Copilot), OpenAI itself, and local
// servers such as Ollama, llama.cpp, LM Studio, and vLLM.
type OpenAICompatible struct {
	name    string
	model   string
	baseURL string
	apiKey  string
	client  *http.Client
}

// OpenAIOption configures an OpenAICompatible provider.
type OpenAIOption func(*OpenAICompatible)

// WithHTTPClient sets a custom HTTP client (useful for tests and timeouts).
func WithHTTPClient(c *http.Client) OpenAIOption {
	return func(o *OpenAICompatible) { o.client = c }
}

// WithName overrides the provider's reported name.
func WithName(name string) OpenAIOption {
	return func(o *OpenAICompatible) { o.name = name }
}

// NewOpenAICompatible constructs a provider for an arbitrary OpenAI-compatible
// endpoint. baseURL should not include the trailing "/chat/completions".
func NewOpenAICompatible(baseURL, apiKey, model string, opts ...OpenAIOption) *OpenAICompatible {
	o := &OpenAICompatible{
		name:    "openai-compatible",
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewGitHubModels constructs a provider backed by GitHub Models / Copilot.
// The token is a GitHub PAT with the "models:read" permission. Model names use
// the publisher/model form, e.g. "openai/gpt-4o" or "anthropic/claude-3.5-sonnet".
func NewGitHubModels(token, model string, opts ...OpenAIOption) *OpenAICompatible {
	opts = append([]OpenAIOption{WithName("github-models")}, opts...)
	return NewOpenAICompatible(GitHubModelsBaseURL, token, model, opts...)
}

// NewLocal constructs a provider for a local OpenAI-compatible server (e.g.
// Ollama). apiKey may be empty for servers that do not require auth.
func NewLocal(baseURL, model string, opts ...OpenAIOption) *OpenAICompatible {
	if baseURL == "" {
		baseURL = OllamaBaseURL
	}
	opts = append([]OpenAIOption{WithName("local")}, opts...)
	return NewOpenAICompatible(baseURL, "", model, opts...)
}

func (o *OpenAICompatible) Name() string  { return o.name }
func (o *OpenAICompatible) Model() string { return o.model }

type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (o *OpenAICompatible) Complete(ctx context.Context, req Request) (Response, error) {
	body := openAIChatRequest{
		Model:       o.model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("%s: marshal request: %w", o.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("%s: build request: %w", o.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("%s: do request: %w", o.name, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("%s: read response: %w", o.name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, &APIError{Provider: o.name, StatusCode: resp.StatusCode, Body: string(data)}
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, fmt.Errorf("%s: decode response: %w", o.name, err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: response contained no choices", o.name)
	}

	return Response{
		Content:      parsed.Choices[0].Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}
