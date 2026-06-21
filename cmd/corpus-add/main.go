// Command corpus-add scaffolds a new eval corpus case from a real commit, PR, or
// staged change. It extracts the diff into change.patch and writes a case.yaml
// seeded with the real message as an unlabeled candidate (quality: TODO) plus
// commented stubs, ready for you to label and add good/bad alternatives.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pr-log-reviewer/input"
	"pr-log-reviewer/review"
)

// todoQuality is the placeholder quality written for the extracted message. It
// is intentionally not a valid quality, so the eval loader will flag it until
// you replace it with good|okay|bad.
const todoQuality = "TODO"

type options struct {
	id     string
	corpus string
	repo   string

	commit  string
	staged  bool
	pr      string
	message string

	force bool
}

func main() {
	var o options
	flag.StringVar(&o.id, "id", "", "case ID / directory name (required)")
	flag.StringVar(&o.corpus, "corpus", "eval/corpus", "corpus directory to add the case under")
	flag.StringVar(&o.repo, "repo", "", "git repository directory (default: current dir)")

	flag.StringVar(&o.commit, "commit", "", "add a commit by ref (e.g. HEAD or a SHA)")
	flag.BoolVar(&o.staged, "staged", false, "add the staged (index) changes")
	flag.StringVar(&o.pr, "pr", "", "add a pull request by number, URL, or branch")
	flag.StringVar(&o.message, "message", "", "message to pair with -staged changes")

	flag.BoolVar(&o.force, "force", false, "overwrite an existing case directory")
	flag.Parse()

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.id == "" {
		return fmt.Errorf("-id is required")
	}

	ctx := context.Background()
	change, err := loadChange(ctx, o)
	if err != nil {
		return err
	}
	if strings.TrimSpace(change.Diff) == "" {
		return fmt.Errorf("the extracted change has an empty diff; nothing to add")
	}

	dir := filepath.Join(o.corpus, o.id)
	switch _, statErr := os.Stat(dir); {
	case statErr == nil && !o.force:
		return fmt.Errorf("case %q already exists at %s (use -force to overwrite)", o.id, dir)
	case statErr == nil:
		// overwriting is allowed; fall through
	case !os.IsNotExist(statErr):
		return fmt.Errorf("stat %s: %w", dir, statErr)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create case dir: %w", err)
	}

	patchPath := filepath.Join(dir, "change.patch")
	if err := os.WriteFile(patchPath, []byte(change.Diff), 0o644); err != nil {
		return fmt.Errorf("write change.patch: %w", err)
	}

	yamlPath := filepath.Join(dir, "case.yaml")
	if err := os.WriteFile(yamlPath, []byte(scaffoldCaseYAML(change.Kind, change.Message)), 0o644); err != nil {
		return fmt.Errorf("write case.yaml: %w", err)
	}

	fmt.Printf("Added case %q at %s\n", o.id, dir)
	fmt.Printf("  - change.patch (%d bytes)\n", len(change.Diff))
	fmt.Printf("  - case.yaml (label the TODO message and add good/bad alternatives)\n\n")
	fmt.Printf("Next: edit %s, then validate with:\n  go run ./cmd/eval -mode replay -case %s\n", yamlPath, o.id)
	return nil
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
		return review.Change{}, fmt.Errorf("specify one of -commit, -staged, or -pr")
	}
}

// scaffoldCaseYAML builds a case.yaml seeded with the real message as an
// unlabeled candidate plus commented guidance for adding alternatives.
func scaffoldCaseYAML(kind review.Kind, message string) string {
	k := string(kind)
	if k == "" {
		k = string(review.KindCommit)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "kind: %s\n", k)
	b.WriteString("conventional: false\n")
	b.WriteString("messages:\n")
	b.WriteString("  # The real extracted message. Replace TODO with good|okay|bad. For\n")
	b.WriteString("  # okay/bad add a defects: [..] list from: faithfulness, completeness,\n")
	b.WriteString("  # rationale, clarity, conventions, scope.\n")
	b.WriteString("  - text: |\n")
	b.WriteString(indentBlock(message, "      "))
	fmt.Fprintf(&b, "    quality: %s\n\n", todoQuality)
	b.WriteString("  # Add at least one clearly-good and one clearly-bad alternative for the\n")
	b.WriteString("  # SAME diff so the metrics have something to compare.\n")
	b.WriteString("  # - text: |\n")
	b.WriteString("  #     <a strong message: says what changed and why>\n")
	b.WriteString("  #   quality: good\n")
	b.WriteString("  # - text: |\n")
	b.WriteString("  #     <a poor message: vague, no rationale>\n")
	b.WriteString("  #   quality: bad\n")
	b.WriteString("  #   defects: [clarity, rationale]\n")
	return b.String()
}

// indentBlock renders text as the body of a YAML literal block scalar: every
// line is prefixed with indent, and a trailing newline is guaranteed. Blank
// lines are emitted empty (no trailing whitespace).
func indentBlock(text, indent string) string {
	text = strings.TrimRight(text, "\n")
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
