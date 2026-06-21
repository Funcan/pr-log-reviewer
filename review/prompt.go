package review

import (
	"fmt"
	"strings"

	"pr-log-reviewer/provider"
)

// categoryDescriptions documents each rubric category for the model. The keys
// are the exact category identifiers the model must use in its response.
var categoryDescriptions = map[Category]string{
	Faithfulness: "Does the message accurately describe what the diff actually does? Penalize claims that the diff does not support, or that contradict it.",
	Completeness: "Does the message cover all significant changes in the diff? Penalize silent omissions of notable changes.",
	Rationale:    "Does the message explain WHY the change was made, not just what changed? Penalize messages that state only the mechanics.",
	Clarity:      "Is the message clear, well structured, and concise? Penalize vague, rambling, or confusing wording.",
	Conventions:  "Does the message follow good conventions: a concise imperative subject line, a blank line before the body, and wrapped body text? Penalize obvious violations.",
	Scope:        "Judge whether the message's described footprint matches the diff's ACTUAL footprint. A change touches a set of components/files/areas; the message should make that breadth clear. Penalize when the message names or implies only a subset of what the diff touches (e.g. says the change is 'in the store' or 'the user store' while the diff also edits the API handler), when it is too vague to convey the real breadth, when it overstates the footprint (claiming work the diff does not contain), or when the diff bundles unrelated changes the message does not call out. Score this low whenever a reader would misjudge how far-reaching the change is.",
}

// PromptOptions tunes prompt construction.
type PromptOptions struct {
	// MaxDiffBytes truncates the diff to at most this many bytes (0 = no limit).
	MaxDiffBytes int
	// Conventional requests that the Conventions category also check for
	// Conventional Commits formatting (e.g. "feat:", "fix:").
	Conventional bool
}

// DefaultMaxDiffBytes bounds how much diff is sent to the model by default.
const DefaultMaxDiffBytes = 12000

// BuildPrompt constructs the system and user messages for reviewing change.
func BuildPrompt(change Change, opts PromptOptions) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: systemPrompt(opts)},
		{Role: provider.RoleUser, Content: userPrompt(change, opts)},
	}
}

func systemPrompt(opts PromptOptions) string {
	var b strings.Builder
	b.WriteString("You are a meticulous reviewer of commit messages and pull request descriptions. ")
	b.WriteString("You judge the QUALITY of the message against the actual change (the diff), not the code itself.\n\n")

	b.WriteString(fmt.Sprintf("Score each of the following categories on an integer scale from %d (poor) to %d (excellent):\n", MinScore, MaxScore))
	for _, c := range AllCategories {
		desc := categoryDescriptions[c]
		if c == Conventions && opts.Conventional {
			desc += " Additionally require Conventional Commits formatting (type(scope): summary)."
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", c, desc))
	}

	b.WriteString("\nThen list concrete, actionable findings. Each finding must name the category it relates to, ")
	b.WriteString("a severity of \"info\", \"minor\", or \"major\", what is wrong, and a specific suggestion to fix it. ")
	b.WriteString("Do not invent problems; if the message is good, return few or no findings.\n\n")

	b.WriteString("Respond with a single JSON object and nothing else, in exactly this shape:\n")
	b.WriteString(`{
  "categories": [
    {"category": "faithfulness", "score": 1-5, "rationale": "short justification"}
    // one entry for every category listed above
  ],
  "findings": [
    {"category": "rationale", "severity": "major", "message": "what is wrong", "suggestion": "how to fix it"}
  ],
  "summary": "one or two sentence overall assessment"
}`)
	b.WriteString("\nUse only the category identifiers listed above. Do not include an overall score; it is computed separately.")
	return b.String()
}

func userPrompt(change Change, opts PromptOptions) string {
	kind := "commit message"
	if change.Kind == KindPR {
		kind = "pull request description"
	}

	diff := change.Diff
	maxBytes := opts.MaxDiffBytes
	if maxBytes > 0 && len(diff) > maxBytes {
		diff = diff[:maxBytes] + "\n... [diff truncated] ..."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Review the following %s against its diff.\n\n", kind))
	b.WriteString("=== MESSAGE ===\n")
	b.WriteString(change.Message)
	b.WriteString("\n\n=== DIFF ===\n")
	b.WriteString(diff)
	b.WriteString("\n")
	return b.String()
}
