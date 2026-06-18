// Command provider-test is a small CLI for exercising the provider package
// against a live or local AI backend. It builds a provider from flags/env,
// sends a single prompt, and prints the response.
//
// Examples:
//
//	# Local Ollama model
//	go run ./cmd/provider-test -provider local -model llama3 -prompt "Say hi in 3 words"
//
//	# GitHub Models / Copilot (needs GITHUB_MODELS_TOKEN or GITHUB_TOKEN)
//	go run ./cmd/provider-test -provider copilot -model openai/gpt-4o -prompt "Say hi"
//
//	# Anthropic / Claude (needs ANTHROPIC_API_KEY)
//	go run ./cmd/provider-test -provider claude -model claude-3-5-sonnet-latest -prompt "Say hi"
//
//	# Read the prompt from stdin
//	echo "Summarize Go in one line" | go run ./cmd/provider-test -provider local -model llama3
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"pr-log-reviewer/provider"
)

func main() {
	var (
		kind    = flag.String("provider", "local", "provider kind: copilot, github-models, anthropic|claude, local|ollama, openai")
		model   = flag.String("model", "", "model name (required)")
		baseURL = flag.String("base-url", "", "base URL override (local/openai endpoints)")
		apiKey  = flag.String("api-key", "", "API key/token (falls back to provider-specific env var)")
		system  = flag.String("system", "", "optional system prompt")
		prompt  = flag.String("prompt", "", "user prompt (if empty, read from stdin)")
		temp    = flag.Float64("temperature", 0, "sampling temperature")
		maxTok  = flag.Int("max-tokens", 512, "max output tokens")
		asJSON  = flag.Bool("json", false, "request a JSON object response")
		record  = flag.String("record", "", "if set, record the interaction to this fixture dir")
		timeout = flag.Duration("timeout", 120*time.Second, "request timeout")
	)
	flag.Parse()

	if err := run(*kind, *model, *baseURL, *apiKey, *system, *prompt, *temp, *maxTok, *asJSON, *record, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(kind, model, baseURL, apiKey, system, prompt string, temp float64, maxTok int, asJSON bool, record string, timeout time.Duration) error {
	userPrompt := prompt
	if userPrompt == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		userPrompt = string(data)
	}
	if userPrompt == "" {
		return fmt.Errorf("no prompt provided (use -prompt or pipe via stdin)")
	}

	p, err := provider.Build(provider.Config{
		Provider: kind,
		Model:    model,
		BaseURL:  baseURL,
		APIKey:   apiKey,
	})
	if err != nil {
		return err
	}
	if record != "" {
		p = provider.NewRecorder(p, record)
	}

	msgs := make([]provider.Message, 0, 2)
	if system != "" {
		msgs = append(msgs, provider.Message{Role: provider.RoleSystem, Content: system})
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: userPrompt})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	resp, err := p.Complete(ctx, provider.Request{
		Messages:    msgs,
		Temperature: temp,
		MaxTokens:   maxTok,
		JSON:        asJSON,
	})
	if err != nil {
		return err
	}
	elapsed := time.Since(start).Round(time.Millisecond)

	fmt.Println(resp.Content)
	fmt.Fprintf(os.Stderr, "\n[%s/%s] in=%d out=%d tokens, %s\n",
		p.Name(), resp.Model, resp.InputTokens, resp.OutputTokens, elapsed)
	return nil
}
