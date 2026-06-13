package sweep

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
)

func writeSweepYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sweep.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAndCells(t *testing.T) {
	path := writeSweepYAML(t, `
target:
  num_ctx: 4096
  prompt_caching: false
matrix:
  temperature: [0, 0.7]
  model: [m-a, m-b]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cells := cfg.Cells()
	if len(cells) != 4 {
		t.Fatalf("cells = %d, want 4", len(cells))
	}

	// Keys sorted (model < temperature), last key varies fastest.
	want := []map[string]any{
		{"num_ctx": 4096, "prompt_caching": false, "model": "m-a", "temperature": 0},
		{"num_ctx": 4096, "prompt_caching": false, "model": "m-a", "temperature": 0.7},
		{"num_ctx": 4096, "prompt_caching": false, "model": "m-b", "temperature": 0},
		{"num_ctx": 4096, "prompt_caching": false, "model": "m-b", "temperature": 0.7},
	}
	for i, cell := range cells {
		if !reflect.DeepEqual(cell, want[i]) {
			t.Errorf("cell %d = %v, want %v", i, cell, want[i])
		}
	}
}

func TestCellsMatrixOverridesTargetDefaults(t *testing.T) {
	cfg := Config{
		Target: map[string]any{"temperature": 0.5, "num_ctx": 2048},
		Matrix: map[string][]any{"temperature": {0.0, 1.0}},
	}
	cells := cfg.Cells()
	if len(cells) != 2 {
		t.Fatalf("cells = %d, want 2", len(cells))
	}
	if cells[0]["temperature"] != 0.0 || cells[1]["temperature"] != 1.0 {
		t.Errorf("matrix should override target default: %v", cells)
	}
	if cells[0]["num_ctx"] != 2048 {
		t.Error("target defaults should carry into every cell")
	}
}

func TestLoadRejectsEmptyMatrix(t *testing.T) {
	if _, err := Load(writeSweepYAML(t, "target: {}\n")); err == nil {
		t.Error("empty matrix should be rejected")
	}
	if _, err := Load(writeSweepYAML(t, "matrix:\n  model: []\n")); err == nil {
		t.Error("empty value list should be rejected")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file should be rejected")
	}
}

func TestCoercions(t *testing.T) {
	if f, ok := Float(3); !ok || f != 3 {
		t.Error("Float(int)")
	}
	if _, ok := Int(2.5); ok {
		t.Error("Int should reject fractional floats")
	}
	if n, ok := Int(16); !ok || n != 16 {
		t.Error("Int(16)")
	}
}

// sweepTestSuite is a minimal one-task suite for integration-light tests.
func sweepTestSuite(t *testing.T) suites.Suite {
	t.Helper()
	manifest := schema.SuiteManifestV1Json{
		Id:      "sweeptest",
		Version: 1,
		Status:  schema.SuiteManifestV1JsonStatusFrozen,
		Title:   "Sweep Test",
		Tracks:  []schema.SuiteManifestV1JsonTracksElem{schema.SuiteManifestV1JsonTracksElemLocal},
		Protocol: schema.SuiteManifestV1JsonProtocol{
			WarmupRequests:   0,
			Repetitions:      1,
			RequestTimeoutMs: 10000,
			Streaming:        true,
			Decoding:         schema.SuiteManifestV1JsonProtocolDecoding{Temperature: 0, MaxTokens: 32},
		},
		Tasks: []schema.SuiteManifestV1JsonTasksElem{{Id: "t", Prompt: "hi"}},
		Scoring: schema.SuiteManifestV1JsonScoring{
			Performance: &schema.SubScoreSpec{Metrics: []schema.SubScoreSpecMetricsElem{
				{Key: "decode_tps_mean", Reference: 100, Weight: 1},
			}},
			CompositeWeights: schema.SuiteManifestV1JsonScoringCompositeWeights{"performance": 1},
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return suites.Suite{Key: "sweeptest-1", Manifest: manifest, Raw: raw}
}

func TestExecuteSharesSweepID(t *testing.T) {
	var seenModels []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{}})
			return
		}
		var body struct {
			Model       string  `json:"model"`
			Temperature float64 `json:"temperature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenModels = append(seenModels, fmt.Sprintf("%s@%v", body.Model, body.Temperature))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"y\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	cfg := Config{
		Matrix: map[string][]any{
			"model":       {"model-a", "model-b"},
			"temperature": {0.0, 0.5},
		},
	}

	var saved []*run.Outcome
	sweepID, cells, err := Execute(context.Background(), cfg, ExecOptions{
		TargetSpec: "openai:base-model",
		TargetOpts: target.Options{BaseURL: srv.URL},
		Base: run.Options{
			Suite:                 sweepTestSuite(t),
			Warmups:               -1,
			Source:                schema.RunV1JsonSourceDev,
			SkipHardwareDetection: true,
		},
		Save: func(o *run.Outcome) error { saved = append(saved, o); return nil },
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !results.ULIDPattern.MatchString(sweepID) {
		t.Errorf("sweep id %q is not a ULID", sweepID)
	}
	if len(cells) != 4 || len(saved) != 4 {
		t.Fatalf("cells = %d, saved = %d, want 4 each", len(cells), len(saved))
	}

	runIDs := map[string]bool{}
	for i, cell := range cells {
		if cell.Err != nil {
			t.Fatalf("cell %d failed: %v", i, cell.Err)
		}
		env := cell.Outcome.Envelope
		if env.SweepId == nil || *env.SweepId != sweepID {
			t.Errorf("cell %d sweep_id = %v, want %s", i, env.SweepId, sweepID)
		}
		runIDs[env.RunId] = true
		// Cell params recorded in the envelope.
		if env.Target.Params["model"] != cell.Params["model"] {
			t.Errorf("cell %d model param = %v, want %v", i, env.Target.Params["model"], cell.Params["model"])
		}
	}
	if len(runIDs) != 4 {
		t.Errorf("expected 4 distinct run ids, got %d", len(runIDs))
	}

	// The model component of the target spec must follow the matrix.
	wantRequests := []string{"model-a@0", "model-a@0.5", "model-b@0", "model-b@0.5"}
	if !reflect.DeepEqual(seenModels, wantRequests) {
		t.Errorf("requests = %v, want %v", seenModels, wantRequests)
	}
}

func TestExecuteRecordsCellErrors(t *testing.T) {
	cfg := Config{Matrix: map[string][]any{"concurrency": {"not-a-number"}}}
	_, cells, err := Execute(context.Background(), cfg, ExecOptions{
		TargetSpec: "openai:m",
		TargetOpts: target.Options{BaseURL: "http://127.0.0.1:1"},
		Base: run.Options{
			Suite:                 sweepTestSuite(t),
			Warmups:               -1,
			Source:                schema.RunV1JsonSourceDev,
			SkipHardwareDetection: true,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(cells) != 1 || cells[0].Err == nil {
		t.Fatalf("expected one failed cell, got %+v", cells)
	}
}
