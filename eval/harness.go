package eval

import (
	"context"
	"fmt"
	"strings"

	"pr-log-reviewer/review"
)

// Reviewer is the subset of *review.Reviewer the harness needs. It is an
// interface so the harness can be driven by fakes or replay-backed reviewers in
// tests without touching the network.
type Reviewer interface {
	Review(ctx context.Context, change review.Change) (review.Review, error)
}

// Harness runs a Reviewer over a corpus and collects per-message results.
type Harness struct {
	reviewer Reviewer
}

// NewHarness builds a Harness around a Reviewer.
func NewHarness(r Reviewer) *Harness {
	return &Harness{reviewer: r}
}

// CaseProgress reports a single message being reviewed. It is optional.
type CaseProgress func(caseID string, msg Message)

// Run reviews every message in the corpus and returns the results in corpus
// order (case order, then message order within a case). The first review error
// aborts the run. If onMsg is non-nil it is called before each message review.
func (h *Harness) Run(ctx context.Context, corpus *Corpus, onMsg CaseProgress) ([]MessageResult, error) {
	var results []MessageResult
	for _, c := range corpus.Cases {
		for _, m := range c.Messages {
			if onMsg != nil {
				onMsg(c.ID, m)
			}
			rev, err := h.reviewer.Review(ctx, c.Change(m))
			if err != nil {
				return nil, fmt.Errorf("eval: review case %q [%s] %q: %w", c.ID, m.Quality, snippet(m.Text), err)
			}
			results = append(results, NewMessageResult(c.ID, m, rev))
		}
	}
	return results, nil
}

// Evaluate runs the harness over the corpus and builds a Report using the given
// thresholds.
func (h *Harness) Evaluate(ctx context.Context, corpus *Corpus, th Thresholds, onMsg CaseProgress) (Report, error) {
	results, err := h.Run(ctx, corpus, onMsg)
	if err != nil {
		return Report{}, err
	}
	return BuildReport(results, th), nil
}

// snippet returns the first line of a message, trimmed and capped, for use in
// error messages so multi-line commit bodies don't bloat the output.
func snippet(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	const max = 60
	if len(line) > max {
		return line[:max] + "…"
	}
	return line
}
