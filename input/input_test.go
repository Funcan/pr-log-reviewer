package input

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"pr-log-reviewer/review"
)

// newTestRepo creates a throwaway git repo and returns its path.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeAndCommit(t *testing.T, dir, file, content, message string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", fmt.Sprintf("cd %q && printf %q > %q", dir, content, file))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write file: %v\n%s", err, out)
	}
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", file)
	run("commit", "-q", "-m", message)
}

func TestFromCommit(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "hello.txt", "hello world\n", "Add greeting\n\nIntroduce a hello file.")

	got, err := New(dir).FromCommit(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("FromCommit: %v", err)
	}
	if got.Kind != review.KindCommit {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Message != "Add greeting\n\nIntroduce a hello file." {
		t.Errorf("Message = %q", got.Message)
	}
	if !strings.Contains(got.Diff, "+hello world") || !strings.Contains(got.Diff, "hello.txt") {
		t.Errorf("Diff missing expected content:\n%s", got.Diff)
	}
	// The commit message must not leak into the diff field.
	if strings.Contains(got.Diff, "Introduce a hello file") {
		t.Errorf("Diff should not contain the commit message:\n%s", got.Diff)
	}
}

func TestFromStaged(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "a.txt", "one\n", "init")

	// Stage a new file without committing.
	if out, err := exec.Command("bash", "-c",
		fmt.Sprintf("cd %q && printf 'two\\n' > b.txt && git add b.txt", dir)).CombinedOutput(); err != nil {
		t.Fatalf("stage: %v\n%s", err, out)
	}

	got, err := New(dir).FromStaged(context.Background(), "Add b file")
	if err != nil {
		t.Fatalf("FromStaged: %v", err)
	}
	if got.Message != "Add b file" {
		t.Errorf("Message = %q", got.Message)
	}
	if !strings.Contains(got.Diff, "b.txt") || !strings.Contains(got.Diff, "+two") {
		t.Errorf("Diff missing staged content:\n%s", got.Diff)
	}
}

func TestFromStaged_NoChanges(t *testing.T) {
	dir := newTestRepo(t)
	writeAndCommit(t, dir, "a.txt", "one\n", "init")

	_, err := New(dir).FromStaged(context.Background(), "msg")
	if err == nil || !strings.Contains(err.Error(), "no staged changes") {
		t.Errorf("expected no-staged-changes error, got %v", err)
	}
}

func TestFromPR_UsesStubbedGH(t *testing.T) {
	e := &Extractor{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			t.Fatalf("expected gh, got %q", name)
		}
		switch args[1] {
		case "view":
			if args[len(args)-1] != "42" {
				t.Errorf("view ref = %q, want 42", args[len(args)-1])
			}
			return []byte("Fix login bug\n\nUsers could not log in."), nil
		case "diff":
			return []byte("diff --git a/auth.go b/auth.go\n+fixed\n"), nil
		}
		return nil, fmt.Errorf("unexpected gh args %v", args)
	}}

	got, err := e.FromPR(context.Background(), "42")
	if err != nil {
		t.Fatalf("FromPR: %v", err)
	}
	if got.Kind != review.KindPR {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Message != "Fix login bug\n\nUsers could not log in." {
		t.Errorf("Message = %q", got.Message)
	}
	if !strings.Contains(got.Diff, "auth.go") {
		t.Errorf("Diff = %q", got.Diff)
	}
}
