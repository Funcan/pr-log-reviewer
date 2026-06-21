package review

import "math"

// AllCategories lists every rubric category in display order.
var AllCategories = []Category{
	Faithfulness,
	Completeness,
	Rationale,
	Clarity,
	Conventions,
	Scope,
}

// DefaultWeights weights each category when computing the aggregate score.
// Rationale — why the change was made, its tradeoffs, and alternatives
// considered — is the most valued signal and carries the most weight. A
// misleading or incomplete message is the next worst, so Faithfulness and
// Completeness follow, and Conventions (a style nit) carries the least.
var DefaultWeights = map[Category]float64{
	Faithfulness: 2.0,
	Completeness: 1.5,
	Rationale:    2.5,
	Clarity:      1.0,
	Conventions:  0.5,
	Scope:        1.0,
}

// clampScore constrains s to the [MinScore, MaxScore] range.
func clampScore(s int) int {
	if s < MinScore {
		return MinScore
	}
	if s > MaxScore {
		return MaxScore
	}
	return s
}

// Aggregate computes the overall 1-5 score as the weighted mean of the per-
// category scores, rounded to the nearest integer. Categories absent from
// weights default to a weight of 1.0; non-positive weights are ignored. It
// returns 0 when there are no usable scores (an invalid Review).
func Aggregate(scores []CategoryScore, weights map[Category]float64) int {
	if weights == nil {
		weights = DefaultWeights
	}

	var weightedSum, totalWeight float64
	for _, cs := range scores {
		w, ok := weights[cs.Category]
		if !ok {
			w = 1.0
		}
		if w <= 0 {
			continue
		}
		weightedSum += w * float64(clampScore(cs.Score))
		totalWeight += w
	}

	if totalWeight == 0 {
		return 0
	}
	return clampScore(int(math.Round(weightedSum / totalWeight)))
}
