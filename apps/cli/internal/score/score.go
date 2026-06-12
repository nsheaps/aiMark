// Package score is the Go port of the canonical aiMark scoring formula
// (packages/scoring/src/index.ts). Both implementations must reproduce the
// golden vectors in packages/schema/testdata/scoring-golden.json.
//
// sub_score = Scale × weighted geometric mean of (metric / reference),
// with lower-is-better metrics inverted before normalization.
// composite  = weighted geometric mean of the present sub-scores; weights
// for absent sub-scores are ignored.
package score

import (
	"fmt"
	"math"
)

// Scale anchors the reference setup at 1000 points.
const Scale = 1000

// MetricSpec mirrors one entry of a suite manifest sub-score spec.
type MetricSpec struct {
	// Key is the metric id as it appears in run metrics, e.g. "decode_tps_mean".
	Key string
	// Reference is the frozen reference value from the suite manifest.
	Reference float64
	// Weight is the relative weight (normalized internally).
	Weight float64
	// LowerIsBetter is true when smaller raw values are better (latencies).
	LowerIsBetter bool
}

// SubScore computes one sub-score from aggregate metrics and the frozen specs.
func SubScore(metrics map[string]float64, specs []MetricSpec) (float64, error) {
	if len(specs) == 0 {
		return 0, fmt.Errorf("score: SubScore requires at least one metric spec")
	}
	totalWeight := 0.0
	for _, s := range specs {
		totalWeight += s.Weight
	}
	logSum := 0.0
	for _, spec := range specs {
		raw, ok := metrics[spec.Key]
		if !ok || raw <= 0 {
			return 0, fmt.Errorf("score: metric %s missing or non-positive", spec.Key)
		}
		normalized := raw / spec.Reference
		if spec.LowerIsBetter {
			normalized = spec.Reference / raw
		}
		logSum += (spec.Weight / totalWeight) * math.Log(normalized)
	}
	return Scale * math.Exp(logSum), nil
}

// Composite combines sub-scores with the manifest composite weights. Weights
// whose sub-score is absent are ignored.
func Composite(subScores map[string]float64, weights map[string]float64) (float64, error) {
	totalWeight := 0.0
	for key, w := range weights {
		if _, ok := subScores[key]; ok {
			totalWeight += w
		}
	}
	if totalWeight <= 0 {
		return 0, fmt.Errorf("score: Composite requires at least one applicable sub-score")
	}
	logSum := 0.0
	for key, weight := range weights {
		value, ok := subScores[key]
		if !ok {
			continue
		}
		if value <= 0 {
			return 0, fmt.Errorf("score: sub-score %s non-positive", key)
		}
		logSum += (weight / totalWeight) * math.Log(value)
	}
	return math.Exp(logSum), nil
}
