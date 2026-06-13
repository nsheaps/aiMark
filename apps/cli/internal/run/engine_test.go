package run

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
)

// newSSEServer speaks just enough of the OpenAI streaming protocol for the
// engine: N content deltas, a usage chunk, then [DONE].
func newSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "test-model"}},
			})
			return
		case "/v1/chat/completions":
		default:
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Stream {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"tok%d \"}}]}\n\n", i)
			flusher.Flush()
			time.Sleep(2 * time.Millisecond)
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

func testSuite(t *testing.T, reps, warmups int) suites.Suite {
	t.Helper()
	lower := true
	manifest := schema.SuiteManifestV1Json{
		Id:      "enginetest",
		Version: 1,
		Status:  schema.SuiteManifestV1JsonStatusFrozen,
		Title:   "Engine Test",
		Tracks:  []schema.SuiteManifestV1JsonTracksElem{schema.SuiteManifestV1JsonTracksElemLocal},
		Protocol: schema.SuiteManifestV1JsonProtocol{
			WarmupRequests:   warmups,
			Repetitions:      reps,
			RequestTimeoutMs: 10000,
			Streaming:        true,
			Decoding:         schema.SuiteManifestV1JsonProtocolDecoding{Temperature: 0, MaxTokens: 64},
		},
		Tasks: []schema.SuiteManifestV1JsonTasksElem{
			{Id: "alpha", Prompt: "say alpha"},
			{Id: "beta", Prompt: "say beta"},
		},
		Scoring: schema.SuiteManifestV1JsonScoring{
			Performance: &schema.SubScoreSpec{Metrics: []schema.SubScoreSpecMetricsElem{
				{Key: "decode_tps_mean", Reference: 100, Weight: 3},
				{Key: "ttft_ms_p50", Reference: 50, Weight: 3, LowerIsBetter: &lower},
			}},
			Consistency: &schema.SubScoreSpec{Metrics: []schema.SubScoreSpecMetricsElem{
				{Key: "ttft_ms_cv", Reference: 0.1, Weight: 1, LowerIsBetter: &lower},
				{Key: "latency_ms_cv", Reference: 0.1, Weight: 1, LowerIsBetter: &lower},
			}},
			CompositeWeights: schema.SuiteManifestV1JsonScoringCompositeWeights{
				"performance": 3,
				"consistency": 1,
			},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return suites.Suite{Key: "enginetest-1", Manifest: manifest, Raw: raw}
}

func TestExecuteEndToEnd(t *testing.T) {
	srv := newSSEServer(t)
	defer srv.Close()

	t.Setenv("CI", "true")
	source, err := DetectSource("")
	if err != nil {
		t.Fatal(err)
	}
	if source != schema.RunV1JsonSourceCi {
		t.Fatalf("DetectSource with CI=true = %q, want ci", source)
	}

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	const reps, warmups = 3, 2
	var progressLines []string
	outcome, err := Execute(context.Background(), Options{
		Suite:                 testSuite(t, reps, warmups),
		Target:                tgt,
		Warmups:               -1,
		Source:                source,
		Params:                map[string]any{"concurrency": 1},
		Progress:              func(format string, args ...any) { progressLines = append(progressLines, fmt.Sprintf(format, args...)) },
		SkipHardwareDetection: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	wantTotal := warmups + reps*2
	if len(outcome.Samples.Samples) != wantTotal {
		t.Fatalf("samples = %d, want %d", len(outcome.Samples.Samples), wantTotal)
	}
	if len(progressLines) != wantTotal {
		t.Errorf("progress lines = %d, want %d", len(progressLines), wantTotal)
	}

	warmupSeen := 0
	for i, s := range outcome.Samples.Samples {
		if s.Status != schema.SamplesV1JsonSamplesElemStatusOk {
			t.Errorf("sample %d status = %s (%v)", i, s.Status, s.Error)
		}
		if s.Warmup != nil && *s.Warmup {
			warmupSeen++
		}
		if s.TtftMs == nil || *s.TtftMs <= 0 {
			t.Errorf("sample %d missing ttft", i)
		}
		if s.LatencyMs == nil || *s.LatencyMs <= 0 {
			t.Errorf("sample %d missing latency", i)
		}
		if s.OutputTokens == nil || *s.OutputTokens != 5 {
			t.Errorf("sample %d output tokens = %v, want 5", i, s.OutputTokens)
		}
	}
	if warmupSeen != warmups {
		t.Errorf("warmup samples = %d, want %d", warmupSeen, warmups)
	}

	env := outcome.Envelope
	// Structural validation against run.v1 requirements.
	if env.SchemaVersion != "aimark.run.v1" {
		t.Errorf("schema_version = %q", env.SchemaVersion)
	}
	if !results.ULIDPattern.MatchString(env.RunId) {
		t.Errorf("run_id = %q, not a ULID", env.RunId)
	}
	if env.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}
	if env.Source != schema.RunV1JsonSourceCi {
		t.Errorf("source = %q, want ci (CI=true was set)", env.Source)
	}
	if env.Cli.Version == "" || env.Cli.Os == "" || env.Cli.Arch == "" {
		t.Errorf("cli block incomplete: %+v", env.Cli)
	}
	if env.Suite.Id != "enginetest" || env.Suite.Version != 1 {
		t.Errorf("suite block = %+v", env.Suite)
	}
	if env.Suite.ProtocolHash == nil || len(*env.Suite.ProtocolHash) != 64 {
		t.Error("protocol_hash missing")
	}
	if env.Target.Kind != schema.RunV1JsonTargetKindLocal {
		t.Errorf("target kind = %q, want local (loopback URL)", env.Target.Kind)
	}
	if env.Target.Model != "test-model" {
		t.Errorf("target model = %q", env.Target.Model)
	}
	if env.Target.Params["concurrency"] == nil || env.Target.Params["max_tokens"] == nil {
		t.Errorf("target params incomplete: %v", env.Target.Params)
	}

	for _, key := range []string{
		"ttft_ms_p50", "ttft_ms_p95", "ttft_ms_p99", "ttft_ms_cv",
		"latency_ms_p50", "latency_ms_p95", "latency_ms_p99", "latency_ms_cv",
		"decode_tps_mean", "inter_token_ms_mean",
	} {
		if _, ok := env.Metrics[key]; !ok {
			t.Errorf("metric %s missing", key)
		}
	}

	if env.ProvisionalScores == nil {
		t.Fatal("provisional_scores missing")
	}
	if env.ProvisionalScores.Performance == nil || env.ProvisionalScores.Consistency == nil {
		t.Error("performance/consistency sub-scores missing")
	}
	if env.ProvisionalScores.Composite == nil || *env.ProvisionalScores.Composite <= 0 {
		t.Error("composite missing")
	}
	if outcome.Composite <= 0 {
		t.Error("Outcome.Composite missing")
	}

	if env.Integrity == nil || env.Integrity.PayloadSha256 == "" || env.Integrity.Nonce == "" {
		t.Fatalf("integrity block incomplete: %+v", env.Integrity)
	}

	// The envelope must round-trip through JSON cleanly.
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var back schema.RunV1Json
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if back.RunId != env.RunId {
		t.Error("envelope did not round-trip")
	}
	// API keys must never appear in results.
	if strings.Contains(string(raw), "Authorization") || strings.Contains(string(raw), "api_key") {
		t.Error("envelope leaks credential-looking fields")
	}
}

func TestDetectSource(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	src, err := DetectSource("")
	if err != nil {
		t.Fatal(err)
	}
	if src != schema.RunV1JsonSourceUser {
		t.Errorf("DetectSource no-CI = %q, want user", src)
	}

	t.Setenv("GITHUB_ACTIONS", "true")
	src, err = DetectSource("")
	if err != nil {
		t.Fatal(err)
	}
	if src != schema.RunV1JsonSourceCi {
		t.Errorf("DetectSource GITHUB_ACTIONS = %q, want ci", src)
	}

	src, err = DetectSource("dev")
	if err != nil {
		t.Fatal(err)
	}
	if src != schema.RunV1JsonSourceDev {
		t.Errorf("DetectSource override dev = %q", src)
	}

	if _, err := DetectSource("bogus"); err == nil {
		t.Error("DetectSource should reject bogus override")
	}
}

func TestExecuteAllRequestsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	tgt, err := target.New("openai:test-model", target.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), Options{
		Suite:                 testSuite(t, 1, 0),
		Target:                tgt,
		Warmups:               -1,
		Source:                schema.RunV1JsonSourceUser,
		SkipHardwareDetection: true,
	})
	if err == nil || !strings.Contains(err.Error(), "every measured request failed") {
		t.Fatalf("expected all-failed error, got %v", err)
	}
}
