// Package eval provides the evaluation harness: it loads a corpus of changes
// paired with good and bad messages, runs the reviewer over them, and reports
// reliability metrics (pairwise discrimination, absolute thresholds, and defect
// label matching).
package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"pr-log-reviewer/review"
)

// Quality labels a candidate message as a good, mediocre-but-acceptable, or bad
// example.
type Quality string

const (
	Good Quality = "good"
	Okay Quality = "okay"
	Bad  Quality = "bad"
)

// Message is a candidate commit/PR message for a given change, with its quality
// label and (for bad messages) the rubric categories it violates.
type Message struct {
	Text    string            `yaml:"text"`
	Quality Quality           `yaml:"quality"`
	Defects []review.Category `yaml:"defects"`
}

// Case is a single change (one diff) with several candidate messages. It is
// loaded from a directory containing change.patch and case.yaml.
type Case struct {
	ID           string    `yaml:"-"`
	Kind         string    `yaml:"kind"`
	Conventional bool      `yaml:"conventional"`
	Messages     []Message `yaml:"messages"`

	Diff string `yaml:"-"` // loaded from change.patch
}

// caseFile mirrors the YAML schema of case.yaml.
type caseFile struct {
	Kind         string    `yaml:"kind"`
	Conventional bool      `yaml:"conventional"`
	Messages     []Message `yaml:"messages"`
}

// Corpus is an ordered collection of cases.
type Corpus struct {
	Cases []Case
}

// LoadCorpus reads every case directory under root. A case directory must
// contain change.patch and case.yaml.
func LoadCorpus(root string) (*Corpus, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("eval: read corpus dir: %w", err)
	}

	corpus := &Corpus{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c, err := LoadCase(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		corpus.Cases = append(corpus.Cases, *c)
	}

	sort.Slice(corpus.Cases, func(i, j int) bool {
		return corpus.Cases[i].ID < corpus.Cases[j].ID
	})

	if len(corpus.Cases) == 0 {
		return nil, fmt.Errorf("eval: no cases found under %s", root)
	}
	return corpus, nil
}

// LoadCase loads a single case directory.
func LoadCase(dir string) (*Case, error) {
	id := filepath.Base(dir)

	rawYAML, err := os.ReadFile(filepath.Join(dir, "case.yaml"))
	if err != nil {
		return nil, fmt.Errorf("eval: case %q: read case.yaml: %w", id, err)
	}
	var cf caseFile
	if err := yaml.Unmarshal(rawYAML, &cf); err != nil {
		return nil, fmt.Errorf("eval: case %q: parse case.yaml: %w", id, err)
	}

	diff, err := os.ReadFile(filepath.Join(dir, "change.patch"))
	if err != nil {
		return nil, fmt.Errorf("eval: case %q: read change.patch: %w", id, err)
	}

	c := &Case{
		ID:           id,
		Kind:         cf.Kind,
		Conventional: cf.Conventional,
		Messages:     cf.Messages,
		Diff:         string(diff),
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// validate checks a case is well formed: it has a diff, at least one good and
// one bad message, valid quality labels, and valid defect categories. Good
// messages must list no defects; bad messages must list at least one; okay
// (mediocre but acceptable) messages may optionally note minor defects.
func (c *Case) validate() error {
	if strings.TrimSpace(c.Diff) == "" {
		return fmt.Errorf("eval: case %q: change.patch is empty", c.ID)
	}
	if len(c.Messages) == 0 {
		return fmt.Errorf("eval: case %q: no messages", c.ID)
	}

	var goods, bads int
	for i, m := range c.Messages {
		if strings.TrimSpace(m.Text) == "" {
			return fmt.Errorf("eval: case %q: message %d has empty text", c.ID, i)
		}
		switch m.Quality {
		case Good:
			goods++
			if len(m.Defects) > 0 {
				return fmt.Errorf("eval: case %q: good message %d must not list defects", c.ID, i)
			}
		case Okay:
			if err := validateDefects(c.ID, i, m.Defects); err != nil {
				return err
			}
		case Bad:
			bads++
			if len(m.Defects) == 0 {
				return fmt.Errorf("eval: case %q: bad message %d must list at least one defect", c.ID, i)
			}
			if err := validateDefects(c.ID, i, m.Defects); err != nil {
				return err
			}
		default:
			return fmt.Errorf("eval: case %q: message %d has invalid quality %q (want good|okay|bad)", c.ID, i, m.Quality)
		}
	}
	if goods == 0 || bads == 0 {
		return fmt.Errorf("eval: case %q: need at least one good and one bad message (have %d good, %d bad)", c.ID, goods, bads)
	}
	return nil
}

// validateDefects checks every defect names a known rubric category.
func validateDefects(caseID string, msgIdx int, defects []review.Category) error {
	for _, d := range defects {
		if !d.Valid() {
			return fmt.Errorf("eval: case %q: message %d has unknown defect %q", caseID, msgIdx, d)
		}
	}
	return nil
}

// Change converts a case message into a review.Change using the case's diff.
func (c *Case) Change(m Message) review.Change {
	kind := review.KindCommit
	if c.Kind == string(review.KindPR) {
		kind = review.KindPR
	}
	return review.Change{Kind: kind, Message: m.Text, Diff: c.Diff}
}
