package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// copilotTokenURL exchanges a GitHub OAuth token for a short-lived Copilot token.
const copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"

// Copilot talks to the GitHub Copilot chat API, which is OpenAI-compatible at
// the /chat/completions path but uses a two-step auth flow: a long-lived GitHub
// OAuth token (created by the Copilot CLI/editor) is exchanged for a short-lived
// Copilot token, and the API host is discovered from that exchange response.
//
// This path works for Copilot subscribers (including enterprise accounts) for
// which the free GitHub Models API is not enabled.
type Copilot struct {
	model         string
	oauthToken    string
	integrationID string
	editorVersion string
	tokenURL      string
	client        *http.Client

	mu        sync.Mutex
	token     string // cached short-lived Copilot token
	apiURL    string // discovered API base, e.g. https://api.business.githubcopilot.com
	expiresAt time.Time
}

// CopilotOption configures a Copilot provider.
type CopilotOption func(*Copilot)

// WithCopilotHTTPClient sets a custom HTTP client (useful for tests).
func WithCopilotHTTPClient(c *http.Client) CopilotOption {
	return func(p *Copilot) { p.client = c }
}

// WithCopilotIntegrationID overrides the Copilot-Integration-Id header.
func WithCopilotIntegrationID(id string) CopilotOption {
	return func(p *Copilot) { p.integrationID = id }
}

// WithCopilotTokenURL overrides the token-exchange URL (used in tests).
func WithCopilotTokenURL(u string) CopilotOption {
	return func(p *Copilot) { p.tokenURL = u }
}

// NewCopilot constructs a Copilot provider from a GitHub OAuth token (the value
// stored by the Copilot CLI). Use CopilotOAuthTokenFromConfig to read it from
// the local Copilot configuration.
func NewCopilot(oauthToken, model string, opts ...CopilotOption) *Copilot {
	p := &Copilot{
		model:         model,
		oauthToken:    oauthToken,
		integrationID: "vscode-chat",
		editorVersion: "pr-log-reviewer/0.1",
		tokenURL:      copilotTokenURL,
		client:        &http.Client{Timeout: 120 * time.Second},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Copilot) Name() string  { return "copilot" }
func (p *Copilot) Model() string { return p.model }

// CopilotOAuthTokenFromConfig reads the GitHub OAuth token saved by the Copilot
// CLI from $XDG_CONFIG_HOME/github-copilot/apps.json (or the equivalent under
// the user's home directory). It returns the first entry's oauth_token.
func CopilotOAuthTokenFromConfig() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("copilot: locate home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}

	// Newer CLIs use apps.json; older ones used hosts.json. Try both.
	for _, name := range []string{"apps.json", "hosts.json"} {
		path := filepath.Join(dir, "github-copilot", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entries map[string]struct {
			OAuthToken string `json:"oauth_token"`
		}
		if err := json.Unmarshal(data, &entries); err != nil {
			return "", fmt.Errorf("copilot: parse %s: %w", path, err)
		}
		for _, e := range entries {
			if e.OAuthToken != "" {
				return e.OAuthToken, nil
			}
		}
	}
	return "", fmt.Errorf("copilot: no oauth token found (run the Copilot CLI to authenticate)")
}

type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// session returns a valid Copilot token and API base URL, refreshing the cached
// token when it is missing or within 60s of expiry.
func (p *Copilot) session(ctx context.Context) (token, apiURL string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Until(p.expiresAt) > time.Minute {
		return p.token, p.apiURL, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.tokenURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("copilot: build token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+p.oauthToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", p.editorVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("copilot: exchange token: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", &APIError{Provider: "copilot", StatusCode: resp.StatusCode, Body: string(data)}
	}

	var tr copilotTokenResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return "", "", fmt.Errorf("copilot: decode token response: %w", err)
	}
	if tr.Token == "" || tr.Endpoints.API == "" {
		return "", "", fmt.Errorf("copilot: token response missing token or api endpoint")
	}

	p.token = tr.Token
	p.apiURL = tr.Endpoints.API
	p.expiresAt = time.Unix(tr.ExpiresAt, 0)
	return p.token, p.apiURL, nil
}

func (p *Copilot) Complete(ctx context.Context, req Request) (Response, error) {
	token, apiURL, err := p.session(ctx)
	if err != nil {
		return Response{}, err
	}

	body := openAIChatRequest{
		Model:       p.model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("copilot: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Response{}, fmt.Errorf("copilot: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Copilot-Integration-Id", p.integrationID)
	httpReq.Header.Set("Editor-Version", p.editorVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("copilot: do request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("copilot: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, &APIError{Provider: "copilot", StatusCode: resp.StatusCode, Body: string(data)}
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, fmt.Errorf("copilot: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("copilot: response contained no choices")
	}

	return Response{
		Content:      parsed.Choices[0].Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}
