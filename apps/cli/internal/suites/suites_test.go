package suites

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceSuitesDir is the canonical manifest location relative to this package.
const sourceSuitesDir = "../../../../packages/suites"

// TestEmbeddedManifestsMatchSource fails when the embedded copies drift from
// packages/suites/*/manifest.json. Run `bun run codegen:suites` in apps/cli
// to resync.
func TestEmbeddedManifestsMatchSource(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(sourceSuitesDir, "*", "manifest.json"))
	if err != nil {
		t.Fatalf("glob source manifests: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("no source manifests found under %s", sourceSuitesDir)
	}

	embeddedEntries, err := embeddedFS.ReadDir("embedded")
	if err != nil {
		t.Fatalf("read embedded dir: %v", err)
	}
	embeddedByName := map[string][]byte{}
	for _, e := range embeddedEntries {
		raw, err := embeddedFS.ReadFile("embedded/" + e.Name())
		if err != nil {
			t.Fatalf("read embedded %s: %v", e.Name(), err)
		}
		embeddedByName[e.Name()] = raw
	}

	seen := map[string]bool{}
	for _, src := range sources {
		key := filepath.Base(filepath.Dir(src)) // e.g. "sprint-1"
		name := key + ".json"
		seen[name] = true
		srcRaw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read source %s: %v", src, err)
		}
		embRaw, ok := embeddedByName[name]
		if !ok {
			t.Errorf("suite %s has no embedded copy; run `bun run codegen:suites` in apps/cli", key)
			continue
		}
		if !bytes.Equal(srcRaw, embRaw) {
			t.Errorf("embedded/%s drifted from %s; run `bun run codegen:suites` in apps/cli", name, src)
		}
	}
	for name := range embeddedByName {
		if !seen[name] {
			t.Errorf("embedded/%s has no source manifest under %s (stale copy?)", name, sourceSuitesDir)
		}
	}
}

func TestAllAndGet(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no embedded suites")
	}

	sprint, err := Get("sprint-1")
	if err != nil {
		t.Fatalf("Get sprint-1: %v", err)
	}
	if sprint.Manifest.Id != "sprint" || sprint.Manifest.Version != 1 {
		t.Fatalf("unexpected manifest identity: %s-%d", sprint.Manifest.Id, sprint.Manifest.Version)
	}
	if len(sprint.Manifest.Tasks) == 0 {
		t.Fatal("sprint-1 has no tasks")
	}
	if sprint.Manifest.Protocol.Repetitions < 1 {
		t.Fatal("sprint-1 protocol repetitions < 1")
	}

	// Bare id resolves when unambiguous.
	byID, err := Get("sprint")
	if err != nil {
		t.Fatalf("Get sprint: %v", err)
	}
	if byID.Key != sprint.Key {
		t.Fatalf("Get(sprint) = %s, want %s", byID.Key, sprint.Key)
	}

	if _, err := Get("nope-99"); err == nil || !strings.Contains(err.Error(), "unknown suite") {
		t.Fatalf("expected unknown suite error, got %v", err)
	}

	hash := sprint.ProtocolHash()
	if len(hash) != 64 {
		t.Fatalf("ProtocolHash length = %d, want 64", len(hash))
	}
}
