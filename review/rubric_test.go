package review

import "testing"

func TestAggregate_WeightedMean(t *testing.T) {
	tests := []struct {
		name    string
		scores  []CategoryScore
		weights map[Category]float64
		want    int
	}{
		{
			name:   "all fives",
			scores: scoresFor(5, 5, 5, 5, 5, 5),
			want:   5,
		},
		{
			name:   "all ones",
			scores: scoresFor(1, 1, 1, 1, 1, 1),
			want:   1,
		},
		{
			name: "faithfulness weighted heavier than conventions",
			// Faithfulness=1 (w2.0), Conventions=5 (w0.5), rest=5.
			scores: []CategoryScore{
				{Category: Faithfulness, Score: 1},
				{Category: Completeness, Score: 5},
				{Category: Rationale, Score: 5},
				{Category: Clarity, Score: 5},
				{Category: Conventions, Score: 5},
				{Category: Scope, Score: 5},
			},
			// (2*1 + 1.5*5 + 2.5*5 + 1*5 + 0.5*5 + 1*5) / 8.5 = 34.5/8.5 = 4.06 -> 4
			want: 4,
		},
		{
			name: "equal weights average",
			scores: []CategoryScore{
				{Category: Faithfulness, Score: 2},
				{Category: Completeness, Score: 4},
			},
			weights: map[Category]float64{Faithfulness: 1, Completeness: 1},
			want:    3,
		},
		{
			name: "out-of-range scores are clamped",
			scores: []CategoryScore{
				{Category: Faithfulness, Score: 9},
				{Category: Completeness, Score: -3},
			},
			weights: map[Category]float64{Faithfulness: 1, Completeness: 1},
			// clamp(9)=5, clamp(-3)=1 -> mean 3
			want: 3,
		},
		{
			name: "zero-weight category ignored",
			scores: []CategoryScore{
				{Category: Faithfulness, Score: 5},
				{Category: Conventions, Score: 1},
			},
			weights: map[Category]float64{Faithfulness: 1, Conventions: 0},
			want:    5,
		},
		{
			name:   "no scores yields invalid zero",
			scores: nil,
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Aggregate(tt.scores, tt.weights); got != tt.want {
				t.Errorf("Aggregate() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAggregate_DefaultWeightsUsedWhenNil(t *testing.T) {
	scores := scoresFor(4, 4, 4, 4, 4, 4)
	if got := Aggregate(scores, nil); got != 4 {
		t.Errorf("Aggregate(nil weights) = %d, want 4", got)
	}
}

func TestCategory_Valid(t *testing.T) {
	for _, c := range AllCategories {
		if !c.Valid() {
			t.Errorf("%q should be valid", c)
		}
	}
	if Category("bogus").Valid() {
		t.Error("bogus category should be invalid")
	}
}

// scoresFor builds CategoryScores for the six categories in AllCategories order.
func scoresFor(vals ...int) []CategoryScore {
	out := make([]CategoryScore, len(vals))
	for i, v := range vals {
		out[i] = CategoryScore{Category: AllCategories[i], Score: v}
	}
	return out
}
