// Package input loads a commit or pull request into a review.Change: the
// message under review plus the actual diff it is meant to describe.
package input

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"pr-log-reviewer/review"
)

// runner executes an external command and returns its stdout. It is a field on
// Extractor so tests can stub git/gh without touching the system.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner runs a real command, returning a useful error (including stderr)
// on failure.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), msg, err)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// Extractor loads changes from git and the GitHub CLI. Use New for the default
// configuration.
type Extractor struct {
	// RepoDir is the working directory for git commands (empty = current dir).
	RepoDir string
	run     runner
}

// New returns an Extractor that runs real git/gh commands in repoDir (empty
// means the current working directory).
func New(repoDir string) *Extractor {
	return &Extractor{RepoDir: repoDir, run: execRunner}
}

// git runs a git subcommand scoped to the extractor's RepoDir.
func (e *Extractor) git(ctx context.Context, args ...string) ([]byte, error) {
	if e.RepoDir != "" {
		args = append([]string{"-C", e.RepoDir}, args...)
	}
	return e.run(ctx, "git", args...)
}

// FromCommit loads the message and diff for a single commit. ref may be any
// git revision (e.g. "HEAD", a SHA, or a tag).
func (e *Extractor) FromCommit(ctx context.Context, ref string) (review.Change, error) {
	if ref == "" {
		ref = "HEAD"
	}

	msg, err := e.git(ctx, "show", "-s", "--format=%B", ref)
	if err != nil {
		return review.Change{}, fmt.Errorf("read commit message: %w", err)
	}

	// --format= with --patch yields the diff only (no commit header). Works for
	// the root commit too, where it diffs against the empty tree.
	diff, err := e.git(ctx, "show", "--format=", "--patch", "--no-color", ref)
	if err != nil {
		return review.Change{}, fmt.Errorf("read commit diff: %w", err)
	}

	return review.Change{
		Kind:    review.KindCommit,
		Message: strings.TrimSpace(string(msg)),
		Diff:    strings.TrimLeft(string(diff), "\n"),
	}, nil
}

// FromStaged loads the staged (index) diff and pairs it with the supplied
// message — useful for reviewing a commit message before it is committed.
func (e *Extractor) FromStaged(ctx context.Context, message string) (review.Change, error) {
	diff, err := e.git(ctx, "diff", "--cached", "--no-color")
	if err != nil {
		return review.Change{}, fmt.Errorf("read staged diff: %w", err)
	}
	d := strings.TrimLeft(string(diff), "\n")
	if strings.TrimSpace(d) == "" {
		return review.Change{}, fmt.Errorf("no staged changes (use `git add` first)")
	}
	return review.Change{
		Kind:    review.KindCommit,
		Message: strings.TrimSpace(message),
		Diff:    d,
	}, nil
}

// FromPR loads a pull request's title+body and its diff via the GitHub CLI.
// ref may be a PR number, URL, or branch (anything `gh pr` accepts); empty
// selects the PR for the current branch.
func (e *Extractor) FromPR(ctx context.Context, ref string) (review.Change, error) {
	viewArgs := []string{"pr", "view", "--json", "title,body",
		"-q", `.title + "\n\n" + .body`}
	diffArgs := []string{"pr", "diff"}
	if ref != "" {
		viewArgs = append(viewArgs, ref)
		diffArgs = append(diffArgs, ref)
	}

	msg, err := e.run(ctx, "gh", viewArgs...)
	if err != nil {
		return review.Change{}, fmt.Errorf("read PR description: %w", err)
	}
	diff, err := e.run(ctx, "gh", diffArgs...)
	if err != nil {
		return review.Change{}, fmt.Errorf("read PR diff: %w", err)
	}

	return review.Change{
		Kind:    review.KindPR,
		Message: strings.TrimSpace(string(msg)),
		Diff:    strings.TrimLeft(string(diff), "\n"),
	}, nil
}
