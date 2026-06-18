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

// AnthropicBaseURL is the default Anthropic API endpoint.
const AnthropicBaseURL = "https://api.anthropic.com"

// anthropicVersion is the required API version header value.
const anthropicVersion = "2023-06-01"

// Anthropic talks to the Anthropic Messages API (Claude). Unlike the OpenAI
// shape, the system prompt is a top-level field and max_tokens is required.
type Anthropic struct {
	model   string
	baseURL string
	apiKey  string
	client  *http.Client
}

// AnthropicOption configures an Anthropic provider.
type AnthropicOption func(*Anthropic)

// WithAnthropicHTTPClient sets a custom HTTP client (useful for tests).
func WithAnthropicHTTPClient(c *http.Client) AnthropicOption {
	return func(a *Anthropic) { a.client = c }
}

// WithAnthropicBaseURL overrides the API base URL.
func WithAnthropicBaseURL(u string) AnthropicOption {
	return func(a *Anthropic) { a.baseURL = strings.TrimRight(u, "/") }
}

// NewAnthropic constructs a Claude provider. model is e.g.
// "claude-3-5-sonnet-latest".
func NewAnthropic(apiKey, model string, opts ...AnthropicOption) *Anthropic {
	a := &Anthropic{
		model:   model,
		baseURL: AnthropicBaseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *Anthropic) Name() string  { return "anthropic" }
func (a *Anthropic) Model() string { return a.model }

type anthropicRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096 // Anthropic requires a positive max_tokens.
	}

	body := anthropicRequest{
		Model:       a.model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      systemPrompt(req.Messages),
		Messages:    nonSystem(req.Messages),
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, &APIError{Provider: "anthropic", StatusCode: resp.StatusCode, Body: string(data)}
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	var sb strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}

	return Response{
		Content:      sb.String(),
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
	}, nil
}
