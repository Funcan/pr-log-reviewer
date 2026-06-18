// Command change-extract turns a commit, staged changes, or a pull request into
// the review.Change form (message + diff) that the AI reviewer consumes. It is a
// development aid for inspecting exactly what will be sent to the model.
//
// Examples:
//
//	go run ./cmd/change-extract -commit HEAD
//	go run ./cmd/change-extract -commit a1b2c3d -repo /path/to/repo
//	go run ./cmd/change-extract -staged -message "Fix the thing"
//	go run ./cmd/change-extract -pr 42
//	go run ./cmd/change-extract -commit HEAD -json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"pr-log-reviewer/input"
	"pr-log-reviewer/review"
)

func main() {
	var (
		commit  = flag.String("commit", "", "review a commit by ref (e.g. HEAD or a SHA)")
		staged  = flag.Bool("staged", false, "review the staged (index) changes")
		pr      = flag.String("pr", "", "review a pull request by number, URL, or branch")
		repo    = flag.String("repo", "", "git repository directory (default: current dir)")
		message = flag.String("message", "", "message to pair with -staged changes")
		asJSON  = flag.Bool("json", false, "emit the Change as JSON")
	)
	flag.Parse()

	change, err := extract(*commit, *staged, *pr, *repo, *message)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(change); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	printHuman(change)
}

func extract(commit string, staged bool, pr, repo, message string) (review.Change, error) {
	e := input.New(repo)
	ctx := context.Background()

	switch {
	case staged:
		return e.FromStaged(ctx, message)
	case pr != "":
		return e.FromPR(ctx, pr)
	case commit != "":
		return e.FromCommit(ctx, commit)
	default:
		// Default to the most recent commit.
		return e.FromCommit(ctx, "HEAD")
	}
}

func printHuman(c review.Change) {
	fmt.Printf("KIND: %s\n", c.Kind)
	fmt.Printf("\n=== MESSAGE ===\n%s\n", c.Message)
	fmt.Printf("\n=== DIFF (%d bytes) ===\n%s\n", len(c.Diff), c.Diff)
}
