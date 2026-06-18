// Package provider defines a minimal, uniform interface over chat-completion
// style AI providers (GitHub Models/Copilot, local OpenAI-compatible servers,
// and Anthropic/Claude), plus record/replay support for deterministic tests.
package provider

import (
	"context"
	"fmt"
)

// Role is the author of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Request is a provider-agnostic completion request. The target model is fixed
// when the provider is constructed; Request carries only generation parameters.
type Request struct {
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	// JSON requests that the model return a single JSON object. Providers that
	// support a native JSON mode use it; others rely on prompt instructions.
	JSON bool `json:"json"`
}

// Response is a provider-agnostic completion result.
type Response struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// Provider is the interface every AI backend implements.
type Provider interface {
	// Name is a stable identifier for the backend (e.g. "github-models").
	Name() string
	// Model is the model the provider was configured with.
	Model() string
	// Complete sends a request and returns the model's response.
	Complete(ctx context.Context, req Request) (Response, error)
}

// APIError describes a non-success HTTP response from a provider.
type APIError struct {
	Provider   string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: http %d: %s", e.Provider, e.StatusCode, e.Body)
}

// systemPrompt returns the first system message content, if any.
func systemPrompt(msgs []Message) string {
	for _, m := range msgs {
		if m.Role == RoleSystem {
			return m.Content
		}
	}
	return ""
}

// nonSystem returns all messages except system messages.
func nonSystem(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != RoleSystem {
			out = append(out, m)
		}
	}
	return out
}
