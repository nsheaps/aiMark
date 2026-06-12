package run

import (
	"math"
	"sort"
)

// percentile computes the q-th percentile (0..1) of values using linear
// interpolation between closest ranks (the common "exclusive of bias"
// definition: rank = q*(n-1)).
func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := q * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// mean returns the arithmetic mean, NaN for empty input.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// cv returns the coefficient of variation (sample stddev / mean). Returns
// NaN when fewer than two values or the mean is zero.
func cv(values []float64) float64 {
	if len(values) < 2 {
		return math.NaN()
	}
	m := mean(values)
	if m == 0 {
		return math.NaN()
	}
	sumSq := 0.0
	for _, v := range values {
		d := v - m
		sumSq += d * d
	}
	stddev := math.Sqrt(sumSq / float64(len(values)-1))
	return stddev / m
}

// aggregate builds the run-level metrics map from measured per-sample values.
// Metrics whose inputs are missing are simply omitted (scoring skips
// sub-scores with missing metrics).
func aggregate(ttftMs, latencyMs, decodeTps, interTokenMs []float64) map[string]float64 {
	metrics := map[string]float64{}
	put := func(key string, value float64) {
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			metrics[key] = value
		}
	}
	put("ttft_ms_p50", percentile(ttftMs, 0.50))
	put("ttft_ms_p95", percentile(ttftMs, 0.95))
	put("ttft_ms_p99", percentile(ttftMs, 0.99))
	put("ttft_ms_cv", cv(ttftMs))
	put("latency_ms_p50", percentile(latencyMs, 0.50))
	put("latency_ms_p95", percentile(latencyMs, 0.95))
	put("latency_ms_p99", percentile(latencyMs, 0.99))
	put("latency_ms_cv", cv(latencyMs))
	put("decode_tps_mean", mean(decodeTps))
	put("inter_token_ms_mean", mean(interTokenMs))
	return metrics
}
