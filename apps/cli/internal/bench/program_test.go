package bench

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

// sourceProgramsDir is the canonical program location relative to this package.
const sourceProgramsDir = "../../../../packages/suites/programs"

// TestEmbeddedProgramsMatchSource fails when the embedded copies drift from
// packages/suites/programs/*/program.json. Run `bun run codegen:suites` in
// apps/cli to resync.
func TestEmbeddedProgramsMatchSource(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join(sourceProgramsDir, "*", "program.json"))
	if err != nil {
		t.Fatalf("glob source programs: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("no source programs found under %s", sourceProgramsDir)
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
		key := filepath.Base(filepath.Dir(src)) // e.g. "bench-1"
		name := key + ".json"
		seen[name] = true
		srcRaw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read source %s: %v", src, err)
		}
		embRaw, ok := embeddedByName[name]
		if !ok {
			t.Errorf("program %s has no embedded copy; run `bun run codegen:suites` in apps/cli", key)
			continue
		}
		if !bytes.Equal(srcRaw, embRaw) {
			t.Errorf("embedded/%s drifted from %s; run `bun run codegen:suites` in apps/cli", name, src)
		}
	}
	for name := range embeddedByName {
		if !seen[name] {
			t.Errorf("embedded/%s has no source program under %s (stale copy?)", name, sourceProgramsDir)
		}
	}
}

func TestGetDefaultProgram(t *testing.T) {
	p, err := Get(DefaultProgram)
	if err != nil {
		t.Fatalf("Get(%s): %v", DefaultProgram, err)
	}
	if p.Manifest.Id != "bench" || p.Manifest.Version != 1 {
		t.Fatalf("unexpected program identity: %s-%d", p.Manifest.Id, p.Manifest.Version)
	}
	if p.Manifest.Status != schema.BenchmarkProgramV1JsonStatusFrozen {
		t.Fatalf("bench-1 must be frozen, got %s", p.Manifest.Status)
	}
	if p.Manifest.AnchorModel == nil || *p.Manifest.AnchorModel != "compact-model" {
		t.Fatalf("anchor_model = %v, want compact-model", p.Manifest.AnchorModel)
	}
	if _, err := Get("nope-9"); err == nil {
		t.Fatal("expected unknown program error")
	}
}

func TestResolveCellsPerClass(t *testing.T) {
	p, err := Get(DefaultProgram)
	if err != nil {
		t.Fatal(err)
	}

	type cellRef struct{ id, model string }
	cases := []struct {
		class string
		want  []cellRef
	}{
		{
			// Compact's class model IS the anchor: no separate anchor cell.
			class: "compact",
			want: []cellRef{
				{"sprint-class", "compact-model"},
				{"marathon-class", "compact-model"},
				{"deepdive-class", "compact-model"},
				{"gauntlet-validity", "compact-model"},
			},
		},
		{
			class: "ultra",
			want: []cellRef{
				{"sprint-class", "ultra-model"},
				{"marathon-class", "ultra-model"},
				{"deepdive-class", "ultra-model"},
				{"sprint-anchor", "compact-model"},
				{"gauntlet-validity", "ultra-model"},
			},
		},
	}
	for _, tc := range cases {
		cells, err := ResolveCells(p.Manifest, tc.class)
		if err != nil {
			t.Fatalf("ResolveCells(%s): %v", tc.class, err)
		}
		if len(cells) != len(tc.want) {
			t.Fatalf("%s: %d cells, want %d", tc.class, len(cells), len(tc.want))
		}
		for i, want := range tc.want {
			if cells[i].ID != want.id || cells[i].Model.Id != want.model {
				t.Errorf("%s cell %d = %s/%s, want %s/%s", tc.class, i, cells[i].ID, cells[i].Model.Id, want.id, want.model)
			}
		}
	}

	// Validity wiring on the gauntlet cell.
	cells, err := ResolveCells(p.Manifest, "compact")
	if err != nil {
		t.Fatal(err)
	}
	gauntlet := cells[len(cells)-1]
	if gauntlet.Role != schema.BenchmarkProgramV1JsonCellsElemRoleValidity {
		t.Fatalf("gauntlet role = %s", gauntlet.Role)
	}
	if gauntlet.ValidityMinAccuracy == nil || *gauntlet.ValidityMinAccuracy != 0.5 {
		t.Fatalf("gauntlet validity_min_accuracy = %v, want 0.5", gauntlet.ValidityMinAccuracy)
	}
	if gauntlet.RepsOverride != 1 {
		t.Fatalf("gauntlet reps_override = %d, want 1", gauntlet.RepsOverride)
	}

	// Score weights as frozen: sprint 3, marathon 2, deepdive 1.
	if cells[0].Weight != 3 || cells[1].Weight != 2 || cells[2].Weight != 1 {
		t.Fatalf("weights = %v %v %v, want 3 2 1", cells[0].Weight, cells[1].Weight, cells[2].Weight)
	}

	if _, err := ResolveCells(p.Manifest, "no-such-class"); err == nil {
		t.Fatal("expected error for unknown class")
	}
}

func TestRuntimeAssetFor(t *testing.T) {
	p, err := Get(DefaultProgram)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		os, arch  string
		cpuOnly   bool
		wantAccel string
		wantPath  string
	}{
		{"darwin", "arm64", false, "metal", "llama-b9616/llama-server"},
		{"linux", "amd64", false, "vulkan", "llama-b9616/llama-server"},
		{"linux", "amd64", true, "cpu", "llama-b9616/llama-server"},
		{"linux", "arm64", true, "cpu", "llama-b9616/llama-server"},
		{"windows", "amd64", false, "cpu", "llama-server.exe"},
		{"windows", "arm64", true, "cpu", "llama-server.exe"},
		{"darwin", "amd64", true, "cpu", "llama-b9616/llama-server"},
	}
	for _, tc := range cases {
		asset, err := RuntimeAssetFor(p.Manifest, tc.os, tc.arch, tc.cpuOnly)
		if err != nil {
			t.Errorf("RuntimeAssetFor(%s/%s cpuOnly=%v): %v", tc.os, tc.arch, tc.cpuOnly, err)
			continue
		}
		if string(asset.Accel) != tc.wantAccel {
			t.Errorf("%s/%s cpuOnly=%v: accel = %s, want %s", tc.os, tc.arch, tc.cpuOnly, asset.Accel, tc.wantAccel)
		}
		if asset.ServerPath == nil || *asset.ServerPath != tc.wantPath {
			t.Errorf("%s/%s: server_path = %v, want %s", tc.os, tc.arch, asset.ServerPath, tc.wantPath)
		}
		if asset.Sha256 == nil || len(*asset.Sha256) != 64 {
			t.Errorf("%s/%s: missing pinned sha256", tc.os, tc.arch)
		}
	}

	if _, err := RuntimeAssetFor(p.Manifest, "plan9", "mips", false); err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}
