package eval

import (
	"math"
	"testing"

	"pr-log-reviewer/review"
)

// res builds a MessageResult with a given score and caught defects.
func res(caseID string, q Quality, score int, defects []review.Category, caught []review.Category) MessageResult {
	return MessageResult{
		CaseID:        caseID,
		Quality:       q,
		Defects:       defects,
		Review:        review.Review{Score: score},
		CaughtDefects: caught,
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPairwise(t *testing.T) {
	results := []MessageResult{
		// case1: good=5 beats bad=2 (correct), good=5 beats bad=4 (correct)
		res("case1", Good, 5, nil, nil),
		res("case1", Bad, 2, []review.Category{review.Clarity}, nil),
		res("case1", Bad, 4, []review.Category{review.Rationale}, nil),
		// case2: good=3 ties bad=3 (tie), good=3 loses to bad=4 (wrong)
		res("case2", Good, 3, nil, nil),
		res("case2", Bad, 3, []review.Category{review.Scope}, nil),
		res("case2", Bad, 4, []review.Category{review.Scope}, nil),
	}
	m := Pairwise(results)
	if m.Correct != 2 || m.Ties != 1 || m.Wrong != 1 {
		t.Fatalf("got correct=%d ties=%d wrong=%d, want 2/1/1", m.Correct, m.Ties, m.Wrong)
	}
	if m.Total() != 4 {
		t.Errorf("Total = %d, want 4", m.Total())
	}
	if !approx(m.Accuracy(), 0.5) {
		t.Errorf("Accuracy = %v, want 0.5", m.Accuracy())
	}
}

func TestPairwise_OnlyComparesWithinCase(t *testing.T) {
	// A good in case1 must not be compared with a bad in case2.
	results := []MessageResult{
		res("case1", Good, 5, nil, nil),
		res("case2", Bad, 1, []review.Category{review.Clarity}, nil),
	}
	m := Pairwise(results)
	if m.Total() != 0 {
		t.Errorf("expected no cross-case pairs, got Total=%d", m.Total())
	}
}

func TestThreshold(t *testing.T) {
	results := []MessageResult{
		res("c", Good, 5, nil, nil),                                // pass (>=4)
		res("c", Good, 3, nil, nil),                                // fail
		res("c", Okay, 3, nil, nil),                                // pass (in 3-4)
		res("c", Okay, 4, nil, nil),                                // pass (in 3-4)
		res("c", Okay, 2, nil, nil),                                // fail (below band)
		res("c", Okay, 5, nil, nil),                                // fail (above band)
		res("c", Bad, 1, []review.Category{review.Clarity}, nil),   // pass (<=2)
		res("c", Bad, 4, []review.Category{review.Rationale}, nil), // fail
	}
	m := Threshold(results, DefaultThresholds)
	if m.GoodPass != 1 || m.GoodTotal != 2 {
		t.Errorf("good = %d/%d, want 1/2", m.GoodPass, m.GoodTotal)
	}
	if m.OkayPass != 2 || m.OkayTotal != 4 {
		t.Errorf("okay = %d/%d, want 2/4", m.OkayPass, m.OkayTotal)
	}
	if m.BadPass != 1 || m.BadTotal != 2 {
		t.Errorf("bad = %d/%d, want 1/2", m.BadPass, m.BadTotal)
	}
	// 4 of 8 pass.
	if !approx(m.PassRate(), 0.5) {
		t.Errorf("PassRate = %v, want 0.5", m.PassRate())
	}
}

func TestLabel_RecallAndPerCategory(t *testing.T) {
	results := []MessageResult{
		// bad with two defects, one caught.
		res("c1", Bad, 2,
			[]review.Category{review.Rationale, review.Clarity},
			[]review.Category{review.Rationale}),
		// bad with one defect, caught.
		res("c2", Bad, 1,
			[]review.Category{review.Clarity},
			[]review.Category{review.Clarity}),
		// good messages are ignored by the label metric.
		res("c1", Good, 5, nil, nil),
	}
	m := Label(results)
	// Tagged defects: rationale(1) + clarity(2) = 3; caught: rationale(1)+clarity(1)=2
	if m.TruePositives != 2 || m.FalseNegatives != 1 {
		t.Fatalf("tp=%d fn=%d, want 2/1", m.TruePositives, m.FalseNegatives)
	}
	if !approx(m.Recall(), 2.0/3.0) {
		t.Errorf("Recall = %v, want 0.667", m.Recall())
	}
	if rc := m.PerCategory[review.Clarity]; rc.Caught != 1 || rc.Total != 2 {
		t.Errorf("clarity recall = %d/%d, want 1/2", rc.Caught, rc.Total)
	}
	if rc := m.PerCategory[review.Rationale]; rc.Caught != 1 || rc.Total != 1 {
		t.Errorf("rationale recall = %d/%d, want 1/1", rc.Caught, rc.Total)
	}
}

func TestNewMessageResult_CatchesViaFindingOrLowScore(t *testing.T) {
	rev := review.Review{
		Score: 2,
		Categories: []review.CategoryScore{
			{Category: review.Rationale, Score: 1}, // low score -> caught
			{Category: review.Clarity, Score: 5},   // high score, but...
			{Category: review.Scope, Score: 4},     // not caught at all
		},
		Findings: []review.Finding{
			{Category: review.Clarity, Severity: review.SeverityMajor, Message: "vague"}, // finding -> caught
		},
	}
	m := Message{
		Quality: Bad,
		Defects: []review.Category{review.Rationale, review.Clarity, review.Scope},
	}
	mr := NewMessageResult("c", m, rev)
	caught := map[review.Category]bool{}
	for _, d := range mr.CaughtDefects {
		caught[d] = true
	}
	if !caught[review.Rationale] {
		t.Error("rationale should be caught via low category score")
	}
	if !caught[review.Clarity] {
		t.Error("clarity should be caught via finding")
	}
	if caught[review.Scope] {
		t.Error("scope should NOT be caught (high score, no finding)")
	}
}

func TestReport_PassesGate(t *testing.T) {
	// Perfect results: good=5, bad=1 with caught defect.
	results := []MessageResult{
		res("c", Good, 5, nil, nil),
		res("c", Bad, 1, []review.Category{review.Clarity}, []review.Category{review.Clarity}),
	}
	rep := BuildReport(results, DefaultThresholds)
	if !rep.Passes(DefaultTargets) {
		t.Errorf("expected perfect results to pass gate: pairwise=%v thresh=%v recall=%v",
			rep.Pairwise.Accuracy(), rep.Threshold.PassRate(), rep.Label.Recall())
	}

	// Now make the bad message score high and uncaught -> should fail all.
	bad := []MessageResult{
		res("c", Good, 5, nil, nil),
		res("c", Bad, 5, []review.Category{review.Clarity}, nil),
	}
	if BuildReport(bad, DefaultThresholds).Passes(DefaultTargets) {
		t.Error("expected poor results to fail the gate")
	}
}
