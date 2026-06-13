package run

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
)

// newScriptedSSEServer streams a fixed response text per request (split into
// small deltas) plus a usage chunk, OpenAI style. respond picks the text from
// the request prompt.
func newScriptedSSEServer(t *testing.T, respond func(prompt string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
			return
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Stream {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		prompt := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				prompt = m.Content
			}
		}
		text := respond(prompt)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Stream in chunks of up to 8 chars so there are multiple token events.
		for i := 0; i < len(text); i += 8 {
			end := i + 8
			if end > len(text) {
				end = len(text)
			}
			chunk := map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": text[i:end]}}}}
			raw, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":12}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

func gradedSuite(t *testing.T, levels []int) suites.Suite {
	t.Helper()
	tol := 0.001
	manifest := schema.SuiteManifestV1Json{
		Id:      "gradetest",
		Version: 1,
		Status:  schema.SuiteManifestV1JsonStatusFrozen,
		Title:   "Grade Test",
		Tracks:  []schema.SuiteManifestV1JsonTracksElem{schema.SuiteManifestV1JsonTracksElemLocal},
		Protocol: schema.SuiteManifestV1JsonProtocol{
			WarmupRequests:    1,
			Repetitions:       2,
			RequestTimeoutMs:  10000,
			Streaming:         true,
			ConcurrencyLevels: levels,
			Decoding:          schema.SuiteManifestV1JsonProtocolDecoding{Temperature: 0, MaxTokens: 64},
		},
		Tasks: []schema.SuiteManifestV1JsonTasksElem{
			{
				Id:     "math-pass",
				Prompt: "what is 6*7? answer 42",
				Grading: &schema.SuiteManifestV1JsonTasksElemGrading{
					Kind:      schema.SuiteManifestV1JsonTasksElemGradingKindNumericTolerance,
					Expected:  42.0,
					Tolerance: &tol,
				},
			},
			{
				Id:     "contains-fail",
				Prompt: "say nothing useful",
				Grading: &schema.SuiteManifestV1JsonTasksElemGrading{
					Kind:               schema.SuiteManifestV1JsonTasksElemGradingKindContainsAll,
					RequiredSubstrings: []string{"zanzibar"},
				},
			},
		},
		Scoring: schema.SuiteManifestV1JsonScoring{
			Quality: &schema.SubScoreSpec{Metrics: []schema.SubScoreSpecMetricsElem{
				{Key: "quality_accuracy", Reference: 0.85, Weight: 1},
			}},
			Performance: &schema.SubScoreSpec{Metrics: []schema.SubScoreSpecMetricsElem{
				{Key: "decode_tps_mean", Reference: 100, Weight: 1},
			}},
			CompositeWeights: schema.SuiteManifestV1JsonScoringCompositeWeights{
				"quality":     3,
				"performance": 1,
			},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return suites.Suite{Key: "gradetest-1", Manifest: manifest, Raw: raw}
}

func TestExecuteGrading(t *testing.T) {
	srv := newScriptedSSEServer(t, func(prompt string) string {
		if strings.Contains(prompt, "6*7") {
			return "The answer is 42."
		}
		return "I refuse to mention that word in my response today."
	})
	defer srv.Close()

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := Execute(context.Background(), Options{
		Suite:                 gradedSuite(t, nil),
		Target:                tgt,
		Warmups:               -1,
		Source:                schema.RunV1JsonSourceDev,
		SkipHardwareDetection: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// math-pass passes every rep, contains-fail fails every rep → accuracy 0.5.
	acc, ok := outcome.Envelope.Metrics["quality_accuracy"]
	if !ok {
		t.Fatal("quality_accuracy metric missing")
	}
	if acc != 0.5 {
		t.Errorf("quality_accuracy = %v, want 0.5", acc)
	}

	// prefill_tps_mean must be present (usage reports prompt_tokens=40).
	if _, ok := outcome.Envelope.Metrics["prefill_tps_mean"]; !ok {
		t.Error("prefill_tps_mean metric missing despite known input tokens")
	}

	gradedSamples := 0
	for _, s := range outcome.Samples.Samples {
		if s.Grade == nil {
			continue
		}
		gradedSamples++
		if s.Grade.Passed == nil || s.Grade.Detail == nil {
			t.Errorf("sample %s grade incomplete: %+v", s.TaskId, s.Grade)
			continue
		}
		wantPass := s.TaskId == "math-pass"
		if *s.Grade.Passed != wantPass {
			t.Errorf("sample %s grade passed = %v, want %v (%s)", s.TaskId, *s.Grade.Passed, wantPass, *s.Grade.Detail)
		}
	}
	// 1 warmup + 2 reps × 2 tasks, all graded (warmup included in samples).
	if gradedSamples != 5 {
		t.Errorf("graded samples = %d, want 5", gradedSamples)
	}

	if outcome.Envelope.ProvisionalScores == nil || outcome.Envelope.ProvisionalScores.Quality == nil {
		t.Fatal("quality sub-score missing")
	}
	// quality = 1000 × (0.5/0.85)
	got := *outcome.Envelope.ProvisionalScores.Quality
	want := 1000 * (0.5 / 0.85)
	if got < want-1 || got > want+1 {
		t.Errorf("quality sub-score = %v, want ~%v", got, want)
	}
}

func TestExecuteConcurrencyLevels(t *testing.T) {
	srv := newScriptedSSEServer(t, func(string) string { return "tok tok tok tok tok tok" })
	defer srv.Close()

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	suite := gradedSuite(t, []int{1, 4})
	outcome, err := Execute(context.Background(), Options{
		Suite:                 suite,
		Target:                tgt,
		Warmups:               -1,
		Source:                schema.RunV1JsonSourceDev,
		SkipHardwareDetection: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 1 warmup + 2 levels × 2 reps × 2 tasks = 9 samples.
	if got := len(outcome.Samples.Samples); got != 9 {
		t.Fatalf("samples = %d, want 9", got)
	}

	for _, key := range []string{
		"throughput_tps_c1", "throughput_tps_c4",
		"latency_ms_p99_c1", "latency_ms_p99_c4",
		"throughput_scaling",
	} {
		if v, ok := outcome.Envelope.Metrics[key]; !ok || v <= 0 {
			t.Errorf("metric %s missing or non-positive (%v)", key, v)
		}
	}

	counts := map[int]int{}
	for _, s := range outcome.Samples.Samples {
		if s.Warmup != nil && *s.Warmup {
			if s.Concurrency != nil {
				t.Error("warmup sample should not record concurrency")
			}
			continue
		}
		if s.Concurrency == nil {
			t.Fatalf("measured sample %s missing concurrency", s.TaskId)
		}
		counts[*s.Concurrency]++
	}
	if counts[1] != 4 || counts[4] != 4 {
		t.Errorf("per-level sample counts = %v, want 4 at each level", counts)
	}

	// scaling = (t4/t1)/(4/1) — sanity: positive and finite, asserted above.
}

func TestExecuteConcurrencyOverride(t *testing.T) {
	srv := newScriptedSSEServer(t, func(string) string { return "tok tok tok tok" })
	defer srv.Close()

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := Execute(context.Background(), Options{
		Suite:                 gradedSuite(t, []int{1, 4}),
		Target:                tgt,
		Warmups:               0,
		Source:                schema.RunV1JsonSourceDev,
		Concurrency:           []int{2},
		SkipHardwareDetection: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := outcome.Envelope.Metrics["throughput_tps_c2"]; !ok {
		t.Error("throughput_tps_c2 missing — concurrency override ignored")
	}
	if _, ok := outcome.Envelope.Metrics["throughput_tps_c4"]; ok {
		t.Error("throughput_tps_c4 present — manifest levels should be overridden")
	}
	if _, ok := outcome.Envelope.Metrics["throughput_scaling"]; ok {
		t.Error("throughput_scaling should be absent with a single level")
	}
}

func TestExecuteSweepAndDecodingOverrides(t *testing.T) {
	var lastBody struct {
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	temp := 0.7
	maxTok := 33
	outcome, err := Execute(context.Background(), Options{
		Suite:                 testSuite(t, 1, 0),
		Target:                tgt,
		Warmups:               -1,
		Source:                schema.RunV1JsonSourceDev,
		SweepID:               "01HSWEEPSWEEPSWEEPSWEEP000",
		Temperature:           &temp,
		MaxTokens:             &maxTok,
		Params:                map[string]any{"temperature": 0.7, "max_tokens": 33},
		SkipHardwareDetection: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Envelope.SweepId == nil || *outcome.Envelope.SweepId != "01HSWEEPSWEEPSWEEPSWEEP000" {
		t.Error("sweep_id not recorded in envelope")
	}
	if lastBody.Temperature != 0.7 || lastBody.MaxTokens != 33 {
		t.Errorf("decoding overrides not sent: %+v", lastBody)
	}
	if outcome.Envelope.Target.Params["temperature"] != 0.7 {
		t.Errorf("envelope params temperature = %v, want 0.7", outcome.Envelope.Target.Params["temperature"])
	}
}
