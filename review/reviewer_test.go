package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pr-log-reviewer/provider"
)

// fakeProvider returns canned responses (or an error) and records requests. If
// resps is set, successive calls return successive entries (last one repeats);
// otherwise resp is returned for every call.
type fakeProvider struct {
	resp    string
	resps   []string
	err     error
	calls   int
	lastReq provider.Request
	gotJSON bool
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Model() string { return "fake-1" }
func (f *fakeProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	f.lastReq = req
	f.gotJSON = req.JSON
	f.calls++
	if f.err != nil {
		return provider.Response{}, f.err
	}
	content := f.resp
	if len(f.resps) > 0 {
		i := f.calls - 1
		if i >= len(f.resps) {
			i = len(f.resps) - 1
		}
		content = f.resps[i]
	}
	return provider.Response{Content: content, Model: "fake-1"}, nil
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

func TestReview_MalformedJSON(t *testing.T) {
	// A response that looks like JSON (has braces) but is syntactically broken,
	// e.g. truncated by a max-tokens limit. Must surface a decode error rather
	// than panic or silently succeed.
	cases := map[string]string{
		"truncated":      `{"categories": [{"category": "faithfulness", "score": 5,`,
		"trailing comma": `{"categories": [{"category": "clarity", "score": 3},],}`,
		"unquoted key":   `{categories: []}`,
		"only opening":   "```json\n{",
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			fp := &fakeProvider{resp: resp}
			_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
			if err == nil {
				t.Fatalf("expected an error for malformed JSON, got nil")
			}
			if !strings.Contains(err.Error(), "decode model response") &&
				!strings.Contains(err.Error(), "no JSON object") {
				t.Errorf("expected decode/no-JSON error, got %v", err)
			}
		})
	}
}

func TestReview_EmptyResponse(t *testing.T) {
	fp := &fakeProvider{resp: ""}
	_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("expected no-JSON error for empty response, got %v", err)
	}
}

func TestReview_NoValidCategories(t *testing.T) {
	fp := &fakeProvider{resp: `{"categories":[{"category":"bogus","score":3}],"summary":"x"}`}
	_, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "no valid categories") {
		t.Errorf("expected no-valid-categories error, got %v", err)
	}
}

func TestReview_RetryRecovers(t *testing.T) {
	// First response is malformed; the retry returns valid JSON.
	fp := &fakeProvider{resps: []string{`{"categories": [`, goodResponse}}
	rev, err := NewReviewer(fp).Review(context.Background(), sampleChange())
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if rev.Score == 0 {
		t.Error("expected a valid score after retry")
	}
	if fp.calls != 2 {
		t.Errorf("provider called %d times, want 2", fp.calls)
	}
	// The retry must include a corrective instruction.
	last := fp.lastReq.Messages[len(fp.lastReq.Messages)-1].Content
	if !strings.Contains(last, "could not be parsed") {
		t.Errorf("retry request missing corrective instruction, got %q", last)
	}
}

func TestReview_RetryExhausted(t *testing.T) {
	fp := &fakeProvider{resp: "not json at all"}
	_, err := NewReviewer(fp, WithMaxRetries(2)).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "gave up after 3 attempts") {
		t.Fatalf("expected give-up error, got %v", err)
	}
	if fp.calls != 3 {
		t.Errorf("provider called %d times, want 3 (1 + 2 retries)", fp.calls)
	}
}

func TestReview_RetryDisabled(t *testing.T) {
	fp := &fakeProvider{resp: "not json"}
	_, err := NewReviewer(fp, WithMaxRetries(0)).Review(context.Background(), sampleChange())
	if err == nil {
		t.Fatal("expected error")
	}
	if fp.calls != 1 {
		t.Errorf("provider called %d times, want 1 (no retry)", fp.calls)
	}
}

func TestReview_ProviderErrorNotRetried(t *testing.T) {
	fp := &fakeProvider{err: errors.New("boom")}
	_, err := NewReviewer(fp, WithMaxRetries(3)).Review(context.Background(), sampleChange())
	if err == nil || !strings.Contains(err.Error(), "provider call") {
		t.Fatalf("expected provider error, got %v", err)
	}
	if fp.calls != 1 {
		t.Errorf("provider called %d times, want 1 (transport errors not retried)", fp.calls)
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
