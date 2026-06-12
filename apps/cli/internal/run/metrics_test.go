package run

import (
	"math"
	"testing"
)

func TestPercentile(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50}
	cases := []struct {
		q, want float64
	}{
		{0, 10},
		{0.5, 30},
		{1, 50},
		{0.25, 20},
		{0.95, 48}, // rank 3.8 → 40 + 0.8*10
	}
	for _, tc := range cases {
		if got := percentile(vals, tc.q); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("percentile(q=%v) = %v, want %v", tc.q, got, tc.want)
		}
	}
	if got := percentile([]float64{7}, 0.99); got != 7 {
		t.Errorf("percentile single = %v", got)
	}
	if got := percentile(nil, 0.5); !math.IsNaN(got) {
		t.Errorf("percentile empty = %v, want NaN", got)
	}
	// Input must not be mutated.
	unsorted := []float64{3, 1, 2}
	_ = percentile(unsorted, 0.5)
	if unsorted[0] != 3 || unsorted[1] != 1 || unsorted[2] != 2 {
		t.Error("percentile mutated its input")
	}
}

func TestMeanAndCV(t *testing.T) {
	if got := mean([]float64{2, 4, 6}); got != 4 {
		t.Errorf("mean = %v", got)
	}
	if got := mean(nil); !math.IsNaN(got) {
		t.Errorf("mean empty = %v, want NaN", got)
	}

	// Sample stddev of {8,12} = sqrt(8) ≈ 2.8284; mean 10 → cv ≈ 0.28284.
	got := cv([]float64{8, 12})
	want := math.Sqrt2 * 2 / 10
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("cv = %v, want %v", got, want)
	}
	if got := cv([]float64{5, 5, 5}); got != 0 {
		t.Errorf("cv constant = %v, want 0", got)
	}
	if got := cv([]float64{1}); !math.IsNaN(got) {
		t.Errorf("cv single = %v, want NaN", got)
	}
}

func TestAggregate(t *testing.T) {
	metrics := aggregate(
		[]float64{40, 50, 60},   // ttft
		[]float64{800, 900, 1000}, // latency
		[]float64{90, 110},      // decode tps
		[]float64{9, 11},        // inter-token
	)
	for _, key := range []string{
		"ttft_ms_p50", "ttft_ms_p95", "ttft_ms_p99", "ttft_ms_cv",
		"latency_ms_p50", "latency_ms_p95", "latency_ms_p99", "latency_ms_cv",
		"decode_tps_mean", "inter_token_ms_mean",
	} {
		if _, ok := metrics[key]; !ok {
			t.Errorf("metric %s missing", key)
		}
	}
	if metrics["ttft_ms_p50"] != 50 {
		t.Errorf("ttft_ms_p50 = %v", metrics["ttft_ms_p50"])
	}
	if metrics["decode_tps_mean"] != 100 {
		t.Errorf("decode_tps_mean = %v", metrics["decode_tps_mean"])
	}
	if metrics["inter_token_ms_mean"] != 10 {
		t.Errorf("inter_token_ms_mean = %v", metrics["inter_token_ms_mean"])
	}

	// Missing inputs produce omitted metrics, never NaN entries.
	sparse := aggregate(nil, []float64{100, 100}, nil, nil)
	if _, ok := sparse["ttft_ms_p50"]; ok {
		t.Error("ttft_ms_p50 should be omitted when no ttft samples")
	}
	if _, ok := sparse["decode_tps_mean"]; ok {
		t.Error("decode_tps_mean should be omitted when no decode samples")
	}
	for k, v := range sparse {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("metric %s is %v", k, v)
		}
	}
}
