package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Compile-time checks that every backend satisfies Provider.
var (
	_ Provider = (*OpenAICompatible)(nil)
	_ Provider = (*Anthropic)(nil)
	_ Provider = (*Copilot)(nil)
	_ Provider = (*Replayer)(nil)
	_ Provider = (*Recorder)(nil)
)

func TestOpenAICompatible_Complete(t *testing.T) {
	var gotBody openAIChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q, want Bearer tok", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "gpt-4o-mini",
			"choices": [{"message": {"role": "assistant", "content": "{\"score\":5}"}}],
			"usage": {"prompt_tokens": 11, "completion_tokens": 3}
		}`)
	}))
	defer srv.Close()

	p := NewOpenAICompatible(srv.URL, "tok", "gpt-4o-mini")
	resp, err := p.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "hi"}},
		MaxTokens: 256,
		JSON:      true,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != `{"score":5}` {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.InputTokens != 11 || resp.OutputTokens != 3 {
		t.Errorf("usage = %d/%d, want 11/3", resp.InputTokens, resp.OutputTokens)
	}
	if gotBody.ResponseFormat == nil || gotBody.ResponseFormat.Type != "json_object" {
		t.Errorf("expected json_object response_format, got %+v", gotBody.ResponseFormat)
	}
	if gotBody.Model != "gpt-4o-mini" {
		t.Errorf("model = %q", gotBody.Model)
	}
}

func TestOpenAICompatible_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "rate limited")
	}))
	defer srv.Close()

	p := NewOpenAICompatible(srv.URL, "tok", "m")
	_, err := p.Complete(context.Background(), Request{})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
}

func TestAPIError_Message(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "openai envelope with code",
			body: `{"error":{"message":"The requested model is not supported.","code":"model_not_supported","type":"invalid_request_error"}}`,
			want: `copilot: http 400: The requested model is not supported. (model_not_supported)`,
		},
		{
			name: "envelope without code",
			body: `{"error":{"message":"unauthorized"}}`,
			want: `copilot: http 400: unauthorized`,
		},
		{
			name: "non-json body falls back to raw",
			body: "rate limited",
			want: `copilot: http 400: rate limited`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &APIError{Provider: "copilot", StatusCode: 400, Body: tt.body}
			if got := e.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnthropic_Complete(t *testing.T) {
	var gotBody anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "secret" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("anthropic-version = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "claude-3-5-sonnet",
			"content": [{"type": "text", "text": "hello"}],
			"usage": {"input_tokens": 7, "output_tokens": 2}
		}`)
	}))
	defer srv.Close()

	p := NewAnthropic("secret", "claude-3-5-sonnet", WithAnthropicBaseURL(srv.URL))
	resp, err := p.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content = %q", resp.Content)
	}
	if gotBody.System != "sys" {
		t.Errorf("system = %q, want sys (must be top-level)", gotBody.System)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != RoleUser {
		t.Errorf("messages = %+v, want only the user turn", gotBody.Messages)
	}
	if gotBody.MaxTokens <= 0 {
		t.Errorf("max_tokens = %d, want positive default", gotBody.MaxTokens)
	}
}

func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "m",
			"choices": [{"message": {"content": "recorded"}}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1}
		}`)
	}))
	defer srv.Close()

	req := Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}}

	live := NewOpenAICompatible(srv.URL, "tok", "m", WithName("test"))
	rec := NewRecorder(live, dir)
	if _, err := rec.Complete(context.Background(), req); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Replay must return the recorded response without any network call.
	replay := NewReplayer(dir, "test", "m")
	resp, err := replay.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if resp.Content != "recorded" {
		t.Errorf("replayed content = %q", resp.Content)
	}

	// A request that was never recorded must fail clearly.
	_, err = replay.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "other"}}})
	if err == nil || !strings.Contains(err.Error(), "no fixture") {
		t.Errorf("missing-fixture error = %v", err)
	}
}
