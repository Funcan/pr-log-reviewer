package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pr-log-reviewer/review"
)

// writeCase creates a case directory with the given patch and case.yaml content.
func writeCase(t *testing.T, root, id, patch, yamlBody string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "change.patch"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validYAML = `kind: commit
conventional: false
messages:
  - text: "Add divide function to support ratio calculations"
    quality: good
  - text: "wip"
    quality: bad
    defects: [clarity, rationale]
`

const validPatch = "diff --git a/calc.go b/calc.go\n+func divide() {}\n"

func TestLoadCase_Valid(t *testing.T) {
	root := t.TempDir()
	writeCase(t, root, "divide", validPatch, validYAML)

	c, err := LoadCase(filepath.Join(root, "divide"))
	if err != nil {
		t.Fatalf("LoadCase: %v", err)
	}
	if c.ID != "divide" {
		t.Errorf("ID = %q", c.ID)
	}
	if c.Kind != "commit" || c.Conventional {
		t.Errorf("meta = %q/%v", c.Kind, c.Conventional)
	}
	if len(c.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(c.Messages))
	}
	if !strings.Contains(c.Diff, "func divide") {
		t.Errorf("diff not loaded: %q", c.Diff)
	}

	// Change() should pair the message text with the case diff.
	ch := c.Change(c.Messages[1])
	if ch.Message != "wip" || ch.Kind != review.KindCommit {
		t.Errorf("Change = %+v", ch)
	}
	if !strings.Contains(ch.Diff, "func divide") {
		t.Error("Change diff missing")
	}
}

func TestLoadCorpus_SortsAndLoadsAll(t *testing.T) {
	root := t.TempDir()
	writeCase(t, root, "bbb", validPatch, validYAML)
	writeCase(t, root, "aaa", validPatch, validYAML)

	corpus, err := LoadCorpus(root)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(corpus.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(corpus.Cases))
	}
	if corpus.Cases[0].ID != "aaa" || corpus.Cases[1].ID != "bbb" {
		t.Errorf("cases not sorted: %s, %s", corpus.Cases[0].ID, corpus.Cases[1].ID)
	}
}

func TestLoadCase_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		patch   string
		yaml    string
		wantErr string
	}{
		{
			name:  "no bad message",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "good one"
    quality: good
`,
			wantErr: "at least one good and one bad",
		},
		{
			name:  "good with defects",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "good one"
    quality: good
    defects: [clarity]
  - text: "bad"
    quality: bad
    defects: [clarity]
`,
			wantErr: "must not list defects",
		},
		{
			name:  "bad without defects",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "good one"
    quality: good
  - text: "bad"
    quality: bad
`,
			wantErr: "must list at least one defect",
		},
		{
			name:  "unknown defect category",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "good"
    quality: good
  - text: "bad"
    quality: bad
    defects: [bogus]
`,
			wantErr: "unknown defect",
		},
		{
			name:  "invalid quality",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "meh"
    quality: maybe
`,
			wantErr: "invalid quality",
		},
		{
			name:  "okay with unknown defect",
			patch: validPatch,
			yaml: `kind: commit
messages:
  - text: "good"
    quality: good
  - text: "meh but fine"
    quality: okay
    defects: [bogus]
  - text: "bad"
    quality: bad
    defects: [clarity]
`,
			wantErr: "unknown defect",
		},
		{
			name:    "empty patch",
			patch:   "",
			yaml:    validYAML,
			wantErr: "change.patch is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeCase(t, root, "c", tt.patch, tt.yaml)
			_, err := LoadCase(filepath.Join(root, "c"))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadCorpus_EmptyDir(t *testing.T) {
	_, err := LoadCorpus(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no cases found") {
		t.Errorf("err = %v, want no-cases error", err)
	}
}

func TestLoadCase_OkayTier(t *testing.T) {
	const okayYAML = `kind: commit
messages:
  - text: "good one"
    quality: good
  - text: "mediocre but acceptable"
    quality: okay
    defects: [rationale]
  - text: "bad one"
    quality: bad
    defects: [clarity]
`
	root := t.TempDir()
	writeCase(t, root, "mixed", validPatch, okayYAML)

	c, err := LoadCase(filepath.Join(root, "mixed"))
	if err != nil {
		t.Fatalf("LoadCase: %v", err)
	}
	if len(c.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(c.Messages))
	}
	if c.Messages[1].Quality != Okay {
		t.Errorf("message 1 quality = %q, want okay", c.Messages[1].Quality)
	}
}

func TestLoadCase_OkayDoesNotSatisfyGoodOrBad(t *testing.T) {
	// A case of good + okay (no bad) must still fail the good-and-bad rule.
	const noBad = `kind: commit
messages:
  - text: "good one"
    quality: good
  - text: "meh"
    quality: okay
`
	root := t.TempDir()
	writeCase(t, root, "nobad", validPatch, noBad)
	_, err := LoadCase(filepath.Join(root, "nobad"))
	if err == nil || !strings.Contains(err.Error(), "at least one good and one bad") {
		t.Errorf("err = %v, want good-and-bad error", err)
	}
}
