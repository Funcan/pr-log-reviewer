// Package review defines the domain model for reviewing the quality of a commit
// message or PR description against the change it describes, plus the scoring
// rubric. The AI provider scores each rubric category; the aggregate score is
// computed deterministically in Go (see rubric.go).
package review

// Kind distinguishes what is being reviewed.
type Kind string

const (
	KindCommit Kind = "commit"
	KindPR     Kind = "pr"
)

// Category is a single rubric dimension. Categories double as defect labels for
// the evaluation corpus: a "bad" message is tagged with the categories it
// violates, and the reviewer's findings are expected to cover them.
type Category string

const (
	// Faithfulness: the message accurately describes what the diff does.
	Faithfulness Category = "faithfulness"
	// Completeness: all significant changes are covered; no silent omissions.
	Completeness Category = "completeness"
	// Rationale: the message explains the motivation (the "why"), not just the "what".
	Rationale Category = "rationale"
	// Clarity: the message is clear, well structured, and concise.
	Clarity Category = "clarity"
	// Conventions: subject length, imperative mood, body wrapping, issue refs,
	// and (optionally) Conventional Commits formatting.
	Conventions Category = "conventions"
	// Scope: the change is cohesive; unrelated changes are flagged.
	Scope Category = "scope"
)

// Severity ranks how serious a finding is.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityMinor Severity = "minor"
	SeverityMajor Severity = "major"
)

// Change is the input to a review: the message under review and the actual
// change it is meant to describe.
type Change struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
	Diff    string `json:"diff"`
}

// CategoryScore is the model's score (1-5) for a single rubric category, with a
// short justification.
type CategoryScore struct {
	Category  Category `json:"category"`
	Score     int      `json:"score"`
	Rationale string   `json:"rationale"`
}

// Finding is a single actionable piece of feedback, tied to a rubric category.
type Finding struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion"`
}

// Review is the structured output of a review. Score is the overall 1-5 rating,
// computed in Go from Categories (it is not taken directly from the model).
type Review struct {
	Score      int             `json:"score"`
	Categories []CategoryScore `json:"categories"`
	Findings   []Finding       `json:"findings"`
	Summary    string          `json:"summary"`
}

// MinScore and MaxScore bound the 1-5 scale used throughout.
const (
	MinScore = 1
	MaxScore = 5
)

// Valid reports whether c is a known rubric category.
func (c Category) Valid() bool {
	switch c {
	case Faithfulness, Completeness, Rationale, Clarity, Conventions, Scope:
		return true
	}
	return false
}
