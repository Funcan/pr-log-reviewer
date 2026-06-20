// Command plr (pr-log-reviewer) reviews the quality of a commit message or pull
// request description against the change it describes, using a configurable AI
// provider.
//
// Examples:
//
//	plr -commit HEAD
//	plr -commit a1b2c3d -repo /path/to/repo
//	plr -staged -message "Fix login timeout"
//	plr -pr 42
//	plr -commit HEAD -provider gemini -model gemini-2.0-flash
//	plr -commit HEAD -json
//	plr -commit HEAD -fail-under 3      # exit non-zero if score < 3 (for CI)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"pr-log-reviewer/input"
	"pr-log-reviewer/provider"
	"pr-log-reviewer/review"
)

type options struct {
	// input
	commit  string
	staged  bool
	pr      string
	repo    string
	message string
	// provider
	providerKind string
	model        string
	baseURL      string
	apiKey       string
	// review tuning
	temperature  float64
	maxTokens    int
	maxDiffBytes int
	conventional bool
	// output
	asJSON    bool
	failUnder int
}

func main() {
	var o options
	// input
	flag.StringVar(&o.commit, "commit", "", "review a commit by ref (e.g. HEAD or a SHA)")
	flag.BoolVar(&o.staged, "staged", false, "review the staged (index) changes")
	flag.StringVar(&o.pr, "pr", "", "review a pull request by number, URL, or branch")
	flag.StringVar(&o.repo, "repo", "", "git repository directory (default: current dir)")
	flag.StringVar(&o.message, "message", "", "message to pair with -staged changes")
	// provider
	flag.StringVar(&o.providerKind, "provider", "copilot", "AI provider: copilot, github-models, anthropic, gemini, local, openai")
	flag.StringVar(&o.model, "model", "gpt-4o", "model name")
	flag.StringVar(&o.baseURL, "base-url", "", "base URL override (local/openai/gemini)")
	flag.StringVar(&o.apiKey, "api-key", "", "API key/token (falls back to provider-specific env var)")
	// review tuning
	flag.Float64Var(&o.temperature, "temperature", 0, "sampling temperature")
	flag.IntVar(&o.maxTokens, "max-tokens", 1500, "max tokens for the model response")
	flag.IntVar(&o.maxDiffBytes, "max-diff-bytes", review.DefaultMaxDiffBytes, "truncate the diff to this many bytes (0 = no limit)")
	flag.BoolVar(&o.conventional, "conventional", false, "also require Conventional Commits formatting")
	// output
	flag.BoolVar(&o.asJSON, "json", false, "emit the review as JSON")
	flag.IntVar(&o.failUnder, "fail-under", 0, "exit non-zero if the score is below this value (0 = never)")
	flag.Parse()

	code, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(o options) (int, error) {
	ctx := context.Background()

	change, err := loadChange(ctx, o)
	if err != nil {
		return 0, err
	}

	p, err := provider.Build(provider.Config{
		Provider: o.providerKind,
		Model:    o.model,
		BaseURL:  o.baseURL,
		APIKey:   o.apiKey,
	})
	if err != nil {
		return 0, err
	}

	reviewer := review.NewReviewer(p,
		review.WithTemperature(o.temperature),
		review.WithMaxTokens(o.maxTokens),
		review.WithPromptOptions(review.PromptOptions{
			MaxDiffBytes: o.maxDiffBytes,
			Conventional: o.conventional,
		}),
	)

	rev, err := reviewer.Review(ctx, change)
	if err != nil {
		return 0, err
	}

	if o.asJSON {
		if err := emitJSON(rev); err != nil {
			return 0, err
		}
	} else {
		printReview(change, rev)
	}

	if o.failUnder > 0 && rev.Score < o.failUnder {
		return 1, nil
	}
	return 0, nil
}

func loadChange(ctx context.Context, o options) (review.Change, error) {
	e := input.New(o.repo)
	switch {
	case o.staged:
		return e.FromStaged(ctx, o.message)
	case o.pr != "":
		return e.FromPR(ctx, o.pr)
	case o.commit != "":
		return e.FromCommit(ctx, o.commit)
	default:
		return e.FromCommit(ctx, "HEAD")
	}
}

func emitJSON(rev review.Review) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rev)
}

func printReview(change review.Change, rev review.Review) {
	fmt.Printf("Score: %d/%d  (%s)\n", rev.Score, review.MaxScore, change.Kind)
	if rev.Summary != "" {
		fmt.Printf("\n%s\n", rev.Summary)
	}

	fmt.Printf("\nCategories:\n")
	for _, c := range rev.Categories {
		fmt.Printf("  %-13s %d/%d  %s\n", c.Category, c.Score, review.MaxScore, c.Rationale)
	}

	if len(rev.Findings) == 0 {
		fmt.Printf("\nNo findings.\n")
		return
	}
	fmt.Printf("\nFindings:\n")
	for _, f := range rev.Findings {
		fmt.Printf("  [%s] %s\n", labelFor(f), f.Message)
		if f.Suggestion != "" {
			fmt.Printf("      \u2192 %s\n", f.Suggestion)
		}
	}
}

func labelFor(f review.Finding) string {
	return fmt.Sprintf("%s/%s", f.Severity, f.Category)
}
