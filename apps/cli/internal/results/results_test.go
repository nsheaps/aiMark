package results

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func TestNewRunIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NewRunID()
		if !ULIDPattern.MatchString(id) {
			t.Fatalf("NewRunID() = %q, not a valid ULID", id)
		}
		if seen[id] {
			t.Fatalf("NewRunID() produced duplicate %q", id)
		}
		seen[id] = true
	}
}

func TestDefaultDirRespectsEnv(t *testing.T) {
	t.Setenv("AIMARK_DATA_DIR", "/tmp/aimark-test-data")
	dir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/tmp/aimark-test-data", "results") {
		t.Fatalf("DefaultDir = %q", dir)
	}

	t.Setenv("AIMARK_DATA_DIR", "")
	dir, err = DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("aimark", "results")
	if !filepath.IsAbs(dir) || !endsWith(dir, want) {
		t.Fatalf("DefaultDir = %q, want suffix %q", dir, want)
	}
}

func endsWith(path, suffix string) bool {
	return len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix
}

func testEnvelope(id string) schema.RunV1Json {
	return schema.RunV1Json{
		SchemaVersion: "aimark.run.v1",
		RunId:         id,
		CreatedAt:     time.Now().UTC(),
		Source:        schema.RunV1JsonSourceUser,
		Cli: schema.RunV1JsonCli{
			Version: "test",
			Os:      schema.RunV1JsonCliOsLinux,
			Arch:    schema.RunV1JsonCliArchAmd64,
		},
		Suite:   schema.RunV1JsonSuite{Id: "sprint", Version: 1},
		Target:  schema.RunV1JsonTarget{Kind: schema.RunV1JsonTargetKindLocal, Model: "m"},
		Metrics: schema.RunV1JsonMetrics{"ttft_ms_p50": 42},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	id := NewRunID()
	env := testEnvelope(id)
	samples := schema.SamplesV1Json{
		SchemaVersion: "aimark.samples.v1",
		RunId:         id,
		Samples: []schema.SamplesV1JsonSamplesElem{
			{TaskId: "greet", Repetition: 0, StartedAt: time.Now().UTC(), Status: schema.SamplesV1JsonSamplesElemStatusOk},
		},
	}
	if err := store.Save(env, samples); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.LoadEnvelope(id)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	if loaded.RunId != id || loaded.Metrics["ttft_ms_p50"] != 42 {
		t.Fatalf("loaded envelope mismatch: %+v", loaded)
	}

	loadedSamples, err := store.LoadSamples(id)
	if err != nil {
		t.Fatalf("LoadSamples: %v", err)
	}
	if len(loadedSamples.Samples) != 1 || loadedSamples.Samples[0].TaskId != "greet" {
		t.Fatalf("loaded samples mismatch: %+v", loadedSamples)
	}

	ids, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("List = %v", ids)
	}

	pending, err := store.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("Pending = %v, want 1 entry", pending)
	}

	if store.IsSubmitted(id) {
		t.Fatal("IsSubmitted = true before submit")
	}
	receipt := SubmitReceipt{RunID: id, ClaimToken: "tok", PublicURL: "https://aimark.dev/r/x"}
	if err := store.MarkSubmitted(id, receipt); err != nil {
		t.Fatalf("MarkSubmitted: %v", err)
	}
	if !store.IsSubmitted(id) {
		t.Fatal("IsSubmitted = false after submit")
	}
	got, err := store.Receipt(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ClaimToken != "tok" {
		t.Fatalf("Receipt = %+v", got)
	}

	pending, err = store.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("Pending after submit = %v, want empty", pending)
	}
}

func TestSaveRejectsBadID(t *testing.T) {
	store, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := testEnvelope("not-a-ulid")
	if err := store.Save(env, schema.SamplesV1Json{}); err == nil {
		t.Fatal("expected error for invalid run id")
	}
}
