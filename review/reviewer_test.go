package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pr-log-reviewer/provider"
)

// fakeProvider returns a canned response (or error) and records the request.
type fakeProvider struct {
	resp    string
	err     error
	gotReq  provider.Request
	gotJSON bool
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "fake-1" }
func (f *fakeProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	f.gotReq = req
	f.gotJSON = req.JSON
	if f.err != nil {
		return provider.Response{}, f.err
	}
	return provider.Response{Content: f.resp, Model: "fake-1"}, nil
}

const goodResponse = `{
  "categories": [
    {"category": "faithfulness", "score": 5, "rationale": "matches the diff"},
    {"category": "completeness", "score": 4, "rationale": "covers most changes"},
    {"category": "rationale", "score": 2, "rationale": "no why"},
    {"category": "clarity", "score": 4, "rationale": "clear"},
    {"category": "conventions", "score": 5, "rationale": "good subject"},
    {"category": "scope", "score": 5, "rationale": "cohesive"}
  ],
  "findings": [
    {"category": "rationale", "severity": "major", "message": "missing motivation", "suggestion": "explain why"}
  ],
  "summary": "Solid but lacks rationale."
}`

func sampleChange() Change {
	return Change{Kind: KindCommit, Message: "Add greeting", Diff: "diff --git a/x b/x\n+hi\n"}
}

func TestReview_HappyPath(t *testing.T) {
	fp := &fakeProvider{resp: goodResponse}
	rev, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Aggregate of 5,4,2,4,5,5 with default weights:
	// (2*5 + 1.5*4 + 1*2 + 1*4 + 0.5*5 + 1*5) / 7 = 29.5/7 = 4.21 -> 4
	if rev.Score != 4 {
		t.Errorf("Score = %d, want 4", rev.Score)
	}
	if len(rev.Categories) != 6 {
		t.Errorf("got %d categories, want 6", len(rev.Categories))
	}
	if len(rev.Findings) != 1 || rev.Findings[0].Category != Rationale {
		t.Errorf("findings = %+v", rev.Findings)
	}
	if rev.Summary != "Solid but lacks rationale." {
		t.Errorf("summary = %q", rev.Summary)
	}
	// The reviewer must request JSON mode.
	if !fp.gotJSON {
		t.Error("expected JSON mode to be requested")
	}
}

func TestReview_StripsMarkdownFences(t *testing.T) {
	fp := &fakeProvider{resp: "```json\n" + goodResponse + "\n```"}
	rev, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if rev.Score == 0 {
		t.Error("expected a valid score from fenced JSON")
	}
}

func TestReview_DropsInvalidCategoriesAndFindings(t *testing.T) {
	resp := `{
		"categories": [
			{"category": "faithfulness", "score": 9, "rationale": "clamp me"},
			{"category": "bogus", "score": 5, "rationale": "ignore me"}
		],
		"findings": [
			{"category": "nonsense", "severity": "major", "message": "x"},
			{"category": "clarity", "severity": "weird", "message": "vague subject", "suggestion": "tighten"},
			{"category": "clarity", "severity": "minor", "message": ""}
		],
		"summary": "ok"
	}`
	fp := &fakeProvider{resp: resp}
	rev, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(rev.Categories) != 1 || rev.Categories[0].Category != Faithfulness {
		t.Fatalf("categories = %+v, want only faithfulness", rev.Categories)
	}
	if rev.Categories[0].Score != MaxScore {
		t.Errorf("score not clamped: %d", rev.Categories[0].Score)
	}
	// Only the clarity finding with a message survives; severity normalized.
	if len(rev.Findings) != 1 {
		t.Fatalf("findings = %+v, want 1", rev.Findings)
	}
	if rev.Findings[0].Severity != SeverityMinor {
		t.Errorf("severity = %q, want minor (normalized)", rev.Findings[0].Severity)
	}
}

func TestReview_EmptyMessage(t *testing.T) {
	_, err := NewReviewer(&fakeProvider{resp: goodResponse}).
		Review(context.Background(), Change{Kind: KindCommit, Message: "   ", Diff: "x"})
	if err == nil || !strings.Contains(err.Error(), "empty message") {
		t.Errorf("expected empty-message error, got %v", err)
	}
}

func TestReview_ProviderError(t *testing.T) {
	fp := &fakeProvider{err: errors.New("boom")}
	_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "provider call") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func TestReview_NoJSON(t *testing.T) {
	fp := &fakeProvider{resp: "I think this looks fine, no JSON here."}
	_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("expected no-JSON error, got %v", err)
	}
}

func TestReview_NoValidCategories(t *testing.T) {
	fp := &fakeProvider{resp: `{"categories":[{"category":"bogus","score":3}],"summary":"x"}`}
	_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "no valid categories") {
		t.Errorf("expected no-valid-categories error, got %v", err)
	}
}

func TestBuildPrompt_TruncatesDiff(t *testing.T) {
	change := Change{Kind: KindCommit, Message: "m", Diff: strings.Repeat("x", 100)}
	msgs := BuildPrompt(change, PromptOptions{MaxDiffBytes: 10})
	user := msgs[1].Content
	if !strings.Contains(user, "[diff truncated]") {
		t.Errorf("expected truncation marker, got:\n%s", user)
	}
}
