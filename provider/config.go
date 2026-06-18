package provider

import (
	"fmt"
	"os"
	"strings"
)

// OpenAIBaseURL is the default endpoint for the "openai" provider kind.
const OpenAIBaseURL = "https://api.openai.com/v1"

// Config selects and configures a provider. APIKey and BaseURL may be left
// empty to fall back to environment variables / sensible defaults (see Build).
type Config struct {
	// Provider kind: "github-models" (aka "copilot"), "anthropic" (aka
	// "claude"), "local" (aka "ollama"), or "openai" ("openai-compatible").
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

// Build constructs a Provider from cfg, resolving credentials from the
// environment when APIKey is empty:
//
//	github-models : GITHUB_MODELS_TOKEN, then GITHUB_TOKEN
//	copilot       : COPILOT_OAUTH_TOKEN, then the local Copilot CLI config
//	anthropic     : ANTHROPIC_API_KEY
//	gemini        : GEMINI_API_KEY, then GOOGLE_API_KEY
//	openai        : OPENAI_API_KEY
//	local         : no key required
func Build(cfg Config) (Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("provider: model is required")
	}

	switch strings.ToLower(cfg.Provider) {
	case "github-models", "github":
		key := firstNonEmpty(cfg.APIKey, os.Getenv("GITHUB_MODELS_TOKEN"), os.Getenv("GITHUB_TOKEN"))
		if key == "" {
			return nil, fmt.Errorf("provider %q: missing token (set GITHUB_MODELS_TOKEN or GITHUB_TOKEN)", cfg.Provider)
		}
		return NewGitHubModels(key, cfg.Model), nil

	case "copilot":
		token := firstNonEmpty(cfg.APIKey, os.Getenv("COPILOT_OAUTH_TOKEN"))
		if token == "" {
			var err error
			if token, err = CopilotOAuthTokenFromConfig(); err != nil {
				return nil, err
			}
		}
		return NewCopilot(token, cfg.Model), nil

	case "anthropic", "claude":
		key := firstNonEmpty(cfg.APIKey, os.Getenv("ANTHROPIC_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("provider %q: missing token (set ANTHROPIC_API_KEY)", cfg.Provider)
		}
		opts := []AnthropicOption{}
		if cfg.BaseURL != "" {
			opts = append(opts, WithAnthropicBaseURL(cfg.BaseURL))
		}
		return NewAnthropic(key, cfg.Model, opts...), nil

	case "local", "ollama":
		return NewLocal(cfg.BaseURL, cfg.Model), nil

	case "gemini", "google":
		key := firstNonEmpty(cfg.APIKey, os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
		if key == "" {
			return nil, fmt.Errorf("provider %q: missing token (set GEMINI_API_KEY)", cfg.Provider)
		}
		if cfg.BaseURL != "" {
			return NewOpenAICompatible(cfg.BaseURL, key, cfg.Model, WithName("gemini")), nil
		}
		return NewGemini(key, cfg.Model), nil

	case "openai", "openai-compatible":
		base := firstNonEmpty(cfg.BaseURL, OpenAIBaseURL)
		key := firstNonEmpty(cfg.APIKey, os.Getenv("OPENAI_API_KEY"))
		return NewOpenAICompatible(base, key, cfg.Model, WithName("openai")), nil

	case "":
		return nil, fmt.Errorf("provider: kind is required")
	default:
		return nil, fmt.Errorf("provider: unknown kind %q", cfg.Provider)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
