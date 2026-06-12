package score

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

const goldenPath = "../../../../packages/schema/testdata/scoring-golden.json"

type goldenSpec struct {
	Key           string  `json:"key"`
	Reference     float64 `json:"reference"`
	Weight        float64 `json:"weight"`
	LowerIsBetter bool    `json:"lower_is_better"`
}

type goldenFile struct {
	Tolerance     float64 `json:"tolerance"`
	SubScoreCases []struct {
		Name     string             `json:"name"`
		Metrics  map[string]float64 `json:"metrics"`
		Specs    []goldenSpec       `json:"specs"`
		Expected float64            `json:"expected"`
	} `json:"sub_score_cases"`
	CompositeCases []struct {
		Name      string             `json:"name"`
		SubScores map[string]float64 `json:"sub_scores"`
		Weights   map[string]float64 `json:"weights"`
		Expected  float64            `json:"expected"`
	} `json:"composite_cases"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if len(g.SubScoreCases) == 0 || len(g.CompositeCases) == 0 {
		t.Fatal("golden vectors are empty")
	}
	return g
}

func TestGoldenSubScores(t *testing.T) {
	g := loadGolden(t)
	for _, tc := range g.SubScoreCases {
		t.Run(tc.Name, func(t *testing.T) {
			specs := make([]MetricSpec, 0, len(tc.Specs))
			for _, s := range tc.Specs {
				specs = append(specs, MetricSpec(s))
			}
			got, err := SubScore(tc.Metrics, specs)
			if err != nil {
				t.Fatalf("SubScore: %v", err)
			}
			if math.Abs(got-tc.Expected) > g.Tolerance {
				t.Fatalf("SubScore = %v, want %v (tolerance %v)", got, tc.Expected, g.Tolerance)
			}
		})
	}
}

func TestGoldenComposites(t *testing.T) {
	g := loadGolden(t)
	for _, tc := range g.CompositeCases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := Composite(tc.SubScores, tc.Weights)
			if err != nil {
				t.Fatalf("Composite: %v", err)
			}
			if math.Abs(got-tc.Expected) > g.Tolerance {
				t.Fatalf("Composite = %v, want %v (tolerance %v)", got, tc.Expected, g.Tolerance)
			}
		})
	}
}

func TestSubScoreErrors(t *testing.T) {
	if _, err := SubScore(map[string]float64{}, nil); err == nil {
		t.Fatal("expected error for empty specs")
	}
	specs := []MetricSpec{{Key: "x", Reference: 1, Weight: 1}}
	if _, err := SubScore(map[string]float64{}, specs); err == nil {
		t.Fatal("expected error for missing metric")
	}
	if _, err := SubScore(map[string]float64{"x": 0}, specs); err == nil {
		t.Fatal("expected error for non-positive metric")
	}
}

func TestCompositeErrors(t *testing.T) {
	if _, err := Composite(map[string]float64{}, map[string]float64{"performance": 1}); err == nil {
		t.Fatal("expected error when no sub-score is applicable")
	}
	if _, err := Composite(map[string]float64{"performance": 0}, map[string]float64{"performance": 1}); err == nil {
		t.Fatal("expected error for non-positive sub-score")
	}
}
