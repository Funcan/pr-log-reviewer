package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"pr-log-reviewer/review"
)

func TestScaffoldCaseYAML_RoundTripsMessage(t *testing.T) {
	msg := "Add a timeout to the client\n\nThe client could hang forever; bound it.\nDefaults to 30s."
	out := scaffoldCaseYAML(review.KindCommit, msg)

	if !strings.Contains(out, "quality: TODO") {
		t.Errorf("scaffold missing TODO quality:\n%s", out)
	}

	// The generated YAML must parse, and the first message's text must equal the
	// original message exactly (multi-line and blank line preserved).
	var parsed struct {
		Kind     string `yaml:"kind"`
		Messages []struct {
			Text    string `yaml:"text"`
			Quality string `yaml:"quality"`
		} `yaml:"messages"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("generated YAML does not parse: %v\n%s", err, out)
	}
	if parsed.Kind != "commit" {
		t.Errorf("kind = %q, want commit", parsed.Kind)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("got %d messages, want 1 (commented stubs must not parse)", len(parsed.Messages))
	}
	got := strings.TrimRight(parsed.Messages[0].Text, "\n")
	if got != msg {
		t.Errorf("round-tripped text mismatch:\n got: %q\nwant: %q", got, msg)
	}
}

func TestScaffoldCaseYAML_DefaultsKind(t *testing.T) {
	out := scaffoldCaseYAML("", "hello")
	if !strings.HasPrefix(out, "kind: commit\n") {
		t.Errorf("empty kind should default to commit:\n%s", out)
	}
}

func TestIndentBlock_PreservesBlankLines(t *testing.T) {
	got := indentBlock("a\n\nb", "  ")
	want := "  a\n\n  b\n"
	if got != want {
		t.Errorf("indentBlock = %q, want %q", got, want)
	}
}
