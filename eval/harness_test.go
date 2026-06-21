package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pr-log-reviewer/review"
)

// fakeReviewer returns a canned Review keyed by message text, or an error.
type fakeReviewer struct {
	byText map[string]review.Review
	err    error
	calls  int
}

func (f *fakeReviewer) Review(_ context.Context, ch review.Change) (review.Review, error) {
	f.calls++
	if f.err != nil {
		return review.Review{}, f.err
	}
	return f.byText[ch.Message], nil
}

func twoCaseCorpus() *Corpus {
	mk := func(id string) Case {
		return Case{
			ID:   id,
			Kind: "commit",
			Diff: "diff --git a/f b/f\n+x\n",
			Messages: []Message{
				{Text: id + "-good", Quality: Good},
				{Text: id + "-bad", Quality: Bad, Defects: []review.Category{review.Clarity}},
			},
		}
	}
	return &Corpus{Cases: []Case{mk("aaa"), mk("bbb")}}
}

func TestHarness_RunCollectsResultsInOrder(t *testing.T) {
	corpus := twoCaseCorpus()
	fr := &fakeReviewer{byText: map[string]review.Review{
		"aaa-good": {Score: 5},
		"aaa-bad":  {Score: 2, Findings: []review.Finding{{Category: review.Clarity}}},
		"bbb-good": {Score: 4},
		"bbb-bad":  {Score: 1},
	}}

	var seen []string
	results, err := NewHarness(fr).Run(context.Background(), corpus, func(id string, m Message) {
		seen = append(seen, id+"/"+string(m.Quality))
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fr.calls != 4 {
		t.Errorf("calls = %d, want 4", fr.calls)
	}
	want := []string{"aaa-good", "aaa-bad", "bbb-good", "bbb-bad"}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for i, w := range want {
		if results[i].Text != w {
			t.Errorf("result[%d].Text = %q, want %q", i, results[i].Text, w)
		}
	}
	if len(seen) != 4 {
		t.Errorf("progress called %d times, want 4", len(seen))
	}

	// aaa-bad caught its clarity defect via the finding; bbb-bad caught it via
	// the low aggregate? No — detection is per-category, bbb-bad has no clarity
	// signal, so it should be missed.
	if got := results[1].CaughtDefects; len(got) != 1 || got[0] != review.Clarity {
		t.Errorf("aaa-bad CaughtDefects = %v, want [clarity]", got)
	}
	if got := results[3].CaughtDefects; len(got) != 0 {
		t.Errorf("bbb-bad CaughtDefects = %v, want none", got)
	}
}

func TestHarness_RunPropagatesError(t *testing.T) {
	fr := &fakeReviewer{err: errors.New("boom")}
	_, err := NewHarness(fr).Run(context.Background(), twoCaseCorpus(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHarness_Evaluate(t *testing.T) {
	corpus := twoCaseCorpus()
	fr := &fakeReviewer{byText: map[string]review.Review{
		"aaa-good": {Score: 5},
		"aaa-bad":  {Score: 2, Categories: []review.CategoryScore{{Category: review.Clarity, Score: 1}}},
		"bbb-good": {Score: 5},
		"bbb-bad":  {Score: 1, Findings: []review.Finding{{Category: review.Clarity}}},
	}}

	rep, err := NewHarness(fr).Evaluate(context.Background(), corpus, DefaultThresholds, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rep.Pairwise.Accuracy() != 1.0 {
		t.Errorf("pairwise accuracy = %v, want 1.0", rep.Pairwise.Accuracy())
	}
	if rep.Threshold.PassRate() != 1.0 {
		t.Errorf("threshold pass rate = %v, want 1.0", rep.Threshold.PassRate())
	}
	if rep.Label.Recall() != 1.0 {
		t.Errorf("defect recall = %v, want 1.0", rep.Label.Recall())
	}
	if !rep.Passes(DefaultTargets) {
		t.Error("expected report to pass default targets")
	}
}

func TestSnippet(t *testing.T) {
	tests := []struct{ in, want string }{
		{"single line", "single line"},
		{"  subject\n\nbody text", "subject"},
		{strings.Repeat("x", 70), strings.Repeat("x", 60) + "…"},
	}
	for _, tt := range tests {
		if got := snippet(tt.in); got != tt.want {
			t.Errorf("snippet(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
