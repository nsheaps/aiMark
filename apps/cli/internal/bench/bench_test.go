package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/integrity"
	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func ptr[T any](v T) *T { return &v }

// newMockLLM serves a minimal OpenAI-compatible streaming endpoint with
// deterministic pacing so the run engine produces non-zero metrics.
func newMockLLM(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "mock"}}})
		case "/v1/chat/completions":
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			chunk := func(delta map[string]any, finish any) {
				payload, _ := json.Marshal(map[string]any{
					"id":      "chatcmpl-test",
					"model":   req.Model,
					"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
				})
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			}
			chunk(map[string]any{"role": "assistant"}, nil)
			time.Sleep(2 * time.Millisecond) // TTFT
			for i := 0; i < 8; i++ {
				chunk(map[string]any{"content": "tok "}, nil)
				time.Sleep(time.Millisecond) // inter-token gap
			}
			chunk(map[string]any{}, "stop")
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// compactRig is a CPU-only machine (classifies compact everywhere).
func compactRig() *schema.HardwareProfile {
	return &schema.HardwareProfile{
		Os:            "linux",
		Arch:          "amd64",
		RamGb:         ptr(32.0),
		UnifiedMemory: ptr(false),
	}
}

// TestExecuteMockPath is the bench conductor's end-to-end test: external
// runtime URL (no assets, no engine), FAST trim, full envelope assembly.
func TestExecuteMockPath(t *testing.T) {
	srv := newMockLLM(t)
	t.Setenv(EnvRuntimeURL, srv.URL+"/v1")
	t.Setenv(EnvFast, "1")

	program, err := Get(DefaultProgram)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	outcome, err := Execute(ctx, Options{
		Program:         program,
		Source:          schema.RunV1JsonSourceCi,
		HardwareProfile: compactRig(),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	env := outcome.Envelope
	if env.SchemaVersion != "aimark.benchmark.v1" {
		t.Errorf("schema_version = %s", env.SchemaVersion)
	}
	if !results.ULIDPattern.MatchString(env.BenchId) {
		t.Errorf("bench_id %q is not a ULID", env.BenchId)
	}
	if env.Class != "compact" {
		t.Errorf("class = %s, want compact", env.Class)
	}
	if env.Program.Id != "bench" || env.Program.Version != 1 {
		t.Errorf("program = %s-%d", env.Program.Id, env.Program.Version)
	}
	if env.Source != schema.BenchmarkV1JsonSourceCi {
		t.Errorf("source = %s, want ci", env.Source)
	}
	if env.Classification == nil || env.Classification.CpuOnly == nil || !*env.Classification.CpuOnly {
		t.Error("classification should record cpu_only=true")
	}
	if env.Environment == nil || env.Environment.HardwareProfile == nil {
		t.Error("environment.hardware_profile missing")
	}

	// Compact runs 4 cells (its class model IS the anchor).
	if len(env.Cells) != 4 || len(outcome.Cells) != 4 {
		t.Fatalf("cells = %d envelope / %d outcomes, want 4", len(env.Cells), len(outcome.Cells))
	}
	for i, cell := range outcome.Cells {
		runEnv := cell.Outcome.Envelope
		if runEnv.BenchId == nil || *runEnv.BenchId != env.BenchId {
			t.Errorf("cell %s: run envelope bench_id = %v, want %s", cell.Cell.ID, runEnv.BenchId, env.BenchId)
		}
		if env.Cells[i].RunId != runEnv.RunId {
			t.Errorf("cell %s: envelope run_id mismatch", cell.Cell.ID)
		}
		if runEnv.Target.Model != cell.Cell.Model.Id {
			t.Errorf("cell %s: recorded model %s, want program model id %s", cell.Cell.ID, runEnv.Target.Model, cell.Cell.Model.Id)
		}
		if v, ok := runEnv.Target.Params["num_ctx"]; !ok || v == nil {
			t.Errorf("cell %s: params missing num_ctx", cell.Cell.ID)
		}
	}

	// The mock can't answer gauntlet correctly → validity must be flagged,
	// but the benchmark still completes and stays uploadable.
	if !outcome.ValidityFailed {
		t.Error("expected validity failure against the canned mock")
	}

	// Score cells produce composites → benchmark provisional scores exist.
	if outcome.Composite <= 0 {
		t.Errorf("benchmark composite = %f, want > 0", outcome.Composite)
	}
	if env.ProvisionalScores == nil || env.ProvisionalScores.Composite == nil {
		t.Fatal("provisional_scores.composite missing")
	}
	if outcome.Performance <= 0 {
		t.Errorf("benchmark performance = %f, want > 0", outcome.Performance)
	}

	// Envelope must be signed and verifiable.
	ok, err := integrity.VerifyBenchmark(env)
	if err != nil || !ok {
		t.Fatalf("VerifyBenchmark = %v, %v", ok, err)
	}

	// And it must round-trip through the results store.
	store, err := results.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBench(env); err != nil {
		t.Fatalf("SaveBench: %v", err)
	}
	loaded, err := store.LoadBench(env.BenchId)
	if err != nil {
		t.Fatalf("LoadBench: %v", err)
	}
	ok, err = integrity.VerifyBenchmark(loaded)
	if err != nil || !ok {
		t.Fatalf("VerifyBenchmark after round-trip = %v, %v", ok, err)
	}
	benches, err := store.ListBench()
	if err != nil || len(benches) != 1 || benches[0] != env.BenchId {
		t.Fatalf("ListBench = %v, %v", benches, err)
	}
}

func TestExecuteClassOverride(t *testing.T) {
	srv := newMockLLM(t)
	t.Setenv(EnvRuntimeURL, srv.URL+"/v1")
	t.Setenv(EnvFast, "1")

	program, err := Get(DefaultProgram)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	outcome, err := Execute(ctx, Options{
		Program:         program,
		Source:          schema.RunV1JsonSourceDev,
		ClassOverride:   "ultra",
		HardwareProfile: compactRig(),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Envelope.Class != "ultra" {
		t.Fatalf("class = %s, want ultra (override)", outcome.Envelope.Class)
	}
	// Ultra runs the anchor cell too → 5 cells.
	if len(outcome.Cells) != 5 {
		t.Fatalf("cells = %d, want 5", len(outcome.Cells))
	}
	if outcome.Envelope.Classification == nil || outcome.Envelope.Classification.Detail == nil {
		t.Fatal("classification detail missing")
	}

	if _, err := Execute(ctx, Options{
		Program:         program,
		Source:          schema.RunV1JsonSourceDev,
		ClassOverride:   "bogus",
		HardwareProfile: compactRig(),
	}); err == nil {
		t.Fatal("expected error for unknown --class override")
	}
}

func TestGroupByModelPreservesOrder(t *testing.T) {
	a := schema.BenchmarkProgramV1JsonModelsElem{Id: "a"}
	b := schema.BenchmarkProgramV1JsonModelsElem{Id: "b"}
	groups := groupByModel([]ResolvedCell{
		{ID: "1", Model: a}, {ID: "2", Model: b}, {ID: "3", Model: a},
	})
	if len(groups) != 2 || groups[0].model.Id != "a" || groups[1].model.Id != "b" {
		t.Fatalf("unexpected grouping: %+v", groups)
	}
	if len(groups[0].cells) != 2 || len(groups[1].cells) != 1 {
		t.Fatalf("unexpected group sizes: %+v", groups)
	}
}

func TestWeightedGeomean(t *testing.T) {
	mk := func(role schema.BenchmarkProgramV1JsonCellsElemRole, weight, composite float64) CellOutcome {
		return CellOutcome{
			Cell:    ResolvedCell{Role: role, Weight: weight},
			Outcome: &run.Outcome{Composite: composite},
		}
	}
	cells := []CellOutcome{
		mk(schema.BenchmarkProgramV1JsonCellsElemRoleScore, 3, 1000),
		mk(schema.BenchmarkProgramV1JsonCellsElemRoleScore, 1, 2000),
		// validity cells never count, even with a weight
		mk(schema.BenchmarkProgramV1JsonCellsElemRoleValidity, 5, 99999),
	}
	got := weightedGeomean(cells, func(o CellOutcome) float64 { return o.Outcome.Composite })
	// geomean = exp((3*ln(1000) + 1*ln(2000)) / 4) ≈ 1189.2
	if got < 1189 || got > 1190 {
		t.Fatalf("weightedGeomean = %f, want ≈1189.2", got)
	}

	// Zero-valued cells are skipped with weight renormalization.
	cells = append(cells, mk(schema.BenchmarkProgramV1JsonCellsElemRoleScore, 10, 0))
	if got2 := weightedGeomean(cells, func(o CellOutcome) float64 { return o.Outcome.Composite }); got2 != got {
		t.Fatalf("zero cell should be skipped: %f != %f", got2, got)
	}

	if v := weightedGeomean(nil, func(o CellOutcome) float64 { return 0 }); v != 0 {
		t.Fatalf("empty geomean = %f, want 0", v)
	}
}
