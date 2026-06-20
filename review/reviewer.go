package review

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"pr-log-reviewer/provider"
)

// Reviewer reviews a Change using an AI provider and the scoring rubric.
type Reviewer struct {
	provider    provider.Provider
	weights     map[Category]float64
	temperature float64
	maxTokens   int
	prompt      PromptOptions
}

// Option configures a Reviewer.
type Option func(*Reviewer)

// WithWeights overrides the category weights used to aggregate the score.
func WithWeights(w map[Category]float64) Option {
	return func(r *Reviewer) { r.weights = w }
}

// WithTemperature sets the sampling temperature (default 0 for determinism).
func WithTemperature(t float64) Option {
	return func(r *Reviewer) { r.temperature = t }
}

// WithMaxTokens caps the model's response length.
func WithMaxTokens(n int) Option {
	return func(r *Reviewer) { r.maxTokens = n }
}

// WithPromptOptions sets prompt-construction options (diff truncation, etc.).
func WithPromptOptions(o PromptOptions) Option {
	return func(r *Reviewer) { r.prompt = o }
}

// NewReviewer constructs a Reviewer backed by p.
func NewReviewer(p provider.Provider, opts ...Option) *Reviewer {
	r := &Reviewer{
		provider:  p,
		weights:   DefaultWeights,
		maxTokens: 1500,
		prompt:    PromptOptions{MaxDiffBytes: DefaultMaxDiffBytes},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// modelOutput is the JSON shape the model is instructed to return. The overall
// score is intentionally absent; it is computed by Aggregate.
type modelOutput struct {
	Categories []CategoryScore `json:"categories"`
	Findings   []Finding       `json:"findings"`
	Summary    string          `json:"summary"`
}

// Review reviews change and returns a structured Review whose Score is computed
// in Go from the model's per-category scores.
func (r *Reviewer) Review(ctx context.Context, change Change) (Review, error) {
	if strings.TrimSpace(change.Message) == "" {
		return Review{}, fmt.Errorf("review: change has an empty message")
	}

	msgs := BuildPrompt(change, r.prompt)
	resp, err := r.provider.Complete(ctx, provider.Request{
		Messages:    msgs,
		Temperature: r.temperature,
		MaxTokens:   r.maxTokens,
		JSON:        true,
	})
	if err != nil {
		return Review{}, fmt.Errorf("review: provider call: %w", err)
	}

	out, err := parseModelOutput(resp.Content)
	if err != nil {
		return Review{}, err
	}

	categories, err := validateCategories(out.Categories)
	if err != nil {
		return Review{}, err
	}

	return Review{
		Score:      Aggregate(categories, r.weights),
		Categories: categories,
		Findings:   sanitizeFindings(out.Findings),
		Summary:    strings.TrimSpace(out.Summary),
	}, nil
}

// parseModelOutput decodes the model's JSON, tolerating Markdown code fences and
// surrounding prose by extracting the outermost JSON object.
func parseModelOutput(content string) (modelOutput, error) {
	raw := extractJSON(content)
	if raw == "" {
		return modelOutput{}, fmt.Errorf("review: no JSON object found in model response")
	}
	var out modelOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return modelOutput{}, fmt.Errorf("review: decode model response: %w", err)
	}
	return out, nil
}

// extractJSON returns the substring from the first '{' to the last '}', after
// stripping common Markdown code fences. It returns "" if no braces are found.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return s[start : end+1]
}

// validateCategories keeps only known categories, clamps their scores, and
// errors if none are usable.
func validateCategories(in []CategoryScore) ([]CategoryScore, error) {
	out := make([]CategoryScore, 0, len(in))
	for _, cs := range in {
		if !cs.Category.Valid() {
			continue
		}
		cs.Score = clampScore(cs.Score)
		cs.Rationale = strings.TrimSpace(cs.Rationale)
		out = append(out, cs)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("review: model response contained no valid categories")
	}
	return out, nil
}

// sanitizeFindings drops findings with unknown categories and normalizes
// severities, defaulting unknown severities to minor.
func sanitizeFindings(in []Finding) []Finding {
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		if !f.Category.Valid() {
			continue
		}
		switch f.Severity {
		case SeverityInfo, SeverityMinor, SeverityMajor:
		default:
			f.Severity = SeverityMinor
		}
		f.Message = strings.TrimSpace(f.Message)
		f.Suggestion = strings.TrimSpace(f.Suggestion)
		if f.Message == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}
