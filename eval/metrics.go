package eval

import (
	"sort"

	"pr-log-reviewer/review"
)

// Thresholds defines the absolute-score cutoffs for the threshold metric.
type Thresholds struct {
	GoodMin int // good messages should score >= GoodMin
	OkayMin int // okay messages should score in [OkayMin, OkayMax]
	OkayMax int
	BadMax  int // bad messages should score <= BadMax
}

// DefaultThresholds are the agreed cutoffs on the 1-5 scale.
var DefaultThresholds = Thresholds{GoodMin: 4, OkayMin: 3, OkayMax: 4, BadMax: 2}

// detectScoreThreshold is the category-score at or below which a defect counts
// as "caught" even without an explicit finding in that category.
const detectScoreThreshold = 2

// MessageResult is the outcome of reviewing one corpus message.
type MessageResult struct {
	CaseID  string
	Text    string
	Quality Quality
	Defects []review.Category
	Review  review.Review
	// CaughtDefects is the subset of Defects the reviewer flagged (bad msgs only).
	CaughtDefects []review.Category
}

// Score is the aggregate score the reviewer assigned.
func (r MessageResult) Score() int { return r.Review.Score }

// caughtCategories returns the set of categories the reviewer flagged for r,
// either via a finding or a low category score (<= detectScoreThreshold).
func caughtCategories(rev review.Review) map[review.Category]bool {
	caught := make(map[review.Category]bool)
	for _, f := range rev.Findings {
		caught[f.Category] = true
	}
	for _, cs := range rev.Categories {
		if cs.Score <= detectScoreThreshold {
			caught[cs.Category] = true
		}
	}
	return caught
}

// NewMessageResult builds a MessageResult, computing CaughtDefects for bad
// messages from the review's findings and low category scores.
func NewMessageResult(caseID string, m Message, rev review.Review) MessageResult {
	mr := MessageResult{
		CaseID:  caseID,
		Text:    m.Text,
		Quality: m.Quality,
		Defects: m.Defects,
		Review:  rev,
	}
	if m.Quality == Bad {
		caught := caughtCategories(rev)
		for _, d := range m.Defects {
			if caught[d] {
				mr.CaughtDefects = append(mr.CaughtDefects, d)
			}
		}
	}
	return mr
}

// PairwiseMetric measures whether good messages outscore bad ones within the
// same case.
type PairwiseMetric struct {
	Correct int // good > bad
	Ties    int // good == bad
	Wrong   int // good < bad
}

// Total returns the number of (good, bad) pairs evaluated.
func (p PairwiseMetric) Total() int { return p.Correct + p.Ties + p.Wrong }

// Accuracy is the fraction of pairs strictly correctly ordered (ties count as
// not correct). It returns 0 when there are no pairs.
func (p PairwiseMetric) Accuracy() float64 {
	if p.Total() == 0 {
		return 0
	}
	return float64(p.Correct) / float64(p.Total())
}

// Pairwise computes the pairwise discrimination metric over results grouped by
// case: every good message is compared against every bad message in the same
// case.
func Pairwise(results []MessageResult) PairwiseMetric {
	byCase := groupByCase(results)
	var m PairwiseMetric
	for _, rs := range byCase {
		var goods, bads []MessageResult
		for _, r := range rs {
			switch r.Quality {
			case Good:
				goods = append(goods, r)
			case Bad:
				bads = append(bads, r)
			}
		}
		for _, g := range goods {
			for _, b := range bads {
				switch {
				case g.Score() > b.Score():
					m.Correct++
				case g.Score() == b.Score():
					m.Ties++
				default:
					m.Wrong++
				}
			}
		}
	}
	return m
}

// ThresholdMetric measures how many messages fall on the correct side of the
// absolute thresholds.
type ThresholdMetric struct {
	GoodPass  int // good messages scoring >= GoodMin
	GoodTotal int
	OkayPass  int // okay messages scoring within [OkayMin, OkayMax]
	OkayTotal int
	BadPass   int // bad messages scoring <= BadMax
	BadTotal  int
}

// PassRate is the fraction of all messages on the correct side of the
// thresholds. It returns 0 when there are no messages.
func (t ThresholdMetric) PassRate() float64 {
	total := t.GoodTotal + t.OkayTotal + t.BadTotal
	if total == 0 {
		return 0
	}
	return float64(t.GoodPass+t.OkayPass+t.BadPass) / float64(total)
}

// Threshold computes the absolute-threshold metric.
func Threshold(results []MessageResult, th Thresholds) ThresholdMetric {
	var m ThresholdMetric
	for _, r := range results {
		switch r.Quality {
		case Good:
			m.GoodTotal++
			if r.Score() >= th.GoodMin {
				m.GoodPass++
			}
		case Okay:
			m.OkayTotal++
			if r.Score() >= th.OkayMin && r.Score() <= th.OkayMax {
				m.OkayPass++
			}
		case Bad:
			m.BadTotal++
			if r.Score() <= th.BadMax {
				m.BadPass++
			}
		}
	}
	return m
}

// LabelMetric measures how well the reviewer's feedback covers the tagged
// defects of bad messages, both overall and per category.
type LabelMetric struct {
	// TruePositives: tagged defects that were caught.
	TruePositives int
	// FalseNegatives: tagged defects that were missed.
	FalseNegatives int
	// PerCategory maps a category to its (caught, total tagged) counts.
	PerCategory map[review.Category]CategoryRecall
}

// CategoryRecall holds caught/total counts for one category.
type CategoryRecall struct {
	Caught int
	Total  int
}

// Recall returns caught/total for the category.
func (c CategoryRecall) Recall() float64 {
	if c.Total == 0 {
		return 0
	}
	return float64(c.Caught) / float64(c.Total)
}

// Recall is the overall defect recall: caught defects / tagged defects.
func (l LabelMetric) Recall() float64 {
	total := l.TruePositives + l.FalseNegatives
	if total == 0 {
		return 0
	}
	return float64(l.TruePositives) / float64(total)
}

// Label computes the defect label-matching metric over bad messages.
func Label(results []MessageResult) LabelMetric {
	m := LabelMetric{PerCategory: make(map[review.Category]CategoryRecall)}
	for _, r := range results {
		if r.Quality != Bad {
			continue
		}
		caught := make(map[review.Category]bool)
		for _, d := range r.CaughtDefects {
			caught[d] = true
		}
		for _, d := range r.Defects {
			rec := m.PerCategory[d]
			rec.Total++
			if caught[d] {
				rec.Caught++
				m.TruePositives++
			} else {
				m.FalseNegatives++
			}
			m.PerCategory[d] = rec
		}
	}
	return m
}

// Report bundles all three metrics plus the gate outcome.
type Report struct {
	Pairwise  PairwiseMetric
	Threshold ThresholdMetric
	Label     LabelMetric
	Results   []MessageResult
}

// Targets are the minimum acceptable values for the regression gate.
type Targets struct {
	PairwiseAccuracy float64
	ThresholdPass    float64
	DefectRecall     float64
}

// DefaultTargets are the proposed gate defaults.
var DefaultTargets = Targets{
	PairwiseAccuracy: 0.90,
	ThresholdPass:    0.80,
	DefectRecall:     0.70,
}

// Passes reports whether all three metrics meet their targets.
func (r Report) Passes(t Targets) bool {
	return r.Pairwise.Accuracy() >= t.PairwiseAccuracy &&
		r.Threshold.PassRate() >= t.ThresholdPass &&
		r.Label.Recall() >= t.DefectRecall
}

// BuildReport computes all metrics from results.
func BuildReport(results []MessageResult, th Thresholds) Report {
	return Report{
		Pairwise:  Pairwise(results),
		Threshold: Threshold(results, th),
		Label:     Label(results),
		Results:   results,
	}
}

// groupByCase groups results by their CaseID, preserving a stable case order.
func groupByCase(results []MessageResult) map[string][]MessageResult {
	byCase := make(map[string][]MessageResult)
	for _, r := range results {
		byCase[r.CaseID] = append(byCase[r.CaseID], r)
	}
	return byCase
}

// SortedCategories returns the categories in PerCategory in display order.
func (l LabelMetric) SortedCategories() []review.Category {
	cats := make([]review.Category, 0, len(l.PerCategory))
	for c := range l.PerCategory {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	return cats
}
