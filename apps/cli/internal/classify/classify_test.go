package classify

import (
	"math"
	"strings"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func ptr[T any](v T) *T { return &v }

// bench1Classes mirrors the bench-1 program class table.
func bench1Classes() []schema.BenchmarkProgramV1JsonClassesElem {
	return []schema.BenchmarkProgramV1JsonClassesElem{
		{Id: "compact", Title: "Compact", Model: "compact-model", CpuOnly: ptr(true), AccelMemMaxGb: ptr(6.0)},
		{Id: "mainstream", Title: "Mainstream", Model: "mainstream-model", AccelMemMinGb: ptr(6.0), AccelMemMaxGb: ptr(16.0)},
		{Id: "performance", Title: "Performance", Model: "performance-model", AccelMemMinGb: ptr(16.0), AccelMemMaxGb: ptr(24.0)},
		{Id: "ultra", Title: "Ultra", Model: "ultra-model", AccelMemMinGb: ptr(24.0)},
	}
}

func program() schema.BenchmarkProgramV1Json {
	return schema.BenchmarkProgramV1Json{Classes: bench1Classes()}
}

func gpuRig(name string, vramGB float64) schema.HardwareProfile {
	return schema.HardwareProfile{
		Os:            "linux",
		Arch:          "amd64",
		RamGb:         ptr(64.0),
		UnifiedMemory: ptr(false),
		Gpus:          []schema.HardwareProfileGpusElem{{Name: name, VramGb: ptr(vramGB)}},
	}
}

func TestClassifyRigs(t *testing.T) {
	cases := []struct {
		name        string
		hw          schema.HardwareProfile
		wantClass   string
		wantCPUOnly bool
		wantUnified bool
		wantMemGB   float64
	}{
		{
			name:      "RTX 4090 24GB → ultra",
			hw:        gpuRig("NVIDIA GeForce RTX 4090", 24),
			wantClass: "ultra", wantMemGB: 24,
		},
		{
			name: "M3 Max 64GB unified → 44.8 usable → ultra",
			hw: schema.HardwareProfile{
				Os: "darwin", Arch: "arm64",
				RamGb:         ptr(64.0),
				UnifiedMemory: ptr(true),
				Gpus:          []schema.HardwareProfileGpusElem{{Name: "Apple M3 Max"}},
			},
			wantClass: "ultra", wantUnified: true, wantMemGB: 44.8,
		},
		{
			name:      "8GB GPU → mainstream",
			hw:        gpuRig("NVIDIA GeForce RTX 4060", 8),
			wantClass: "mainstream", wantMemGB: 8,
		},
		{
			name: "no GPU, 128GB RAM → compact (CPU-only always compact)",
			hw: schema.HardwareProfile{
				Os: "linux", Arch: "amd64",
				RamGb:         ptr(128.0),
				UnifiedMemory: ptr(false),
			},
			wantClass: "compact", wantCPUOnly: true,
		},
		{
			name:      "4GB GPU → compact via memory bound",
			hw:        gpuRig("NVIDIA GTX 1650", 4),
			wantClass: "compact", wantMemGB: 4,
		},
		{
			name:      "16GB GPU → performance (boundary inclusive at min)",
			hw:        gpuRig("RTX 4080", 16),
			wantClass: "performance", wantMemGB: 16,
		},
		{
			name:      "6GB GPU → mainstream (boundary inclusive at min)",
			hw:        gpuRig("RTX 2060", 6),
			wantClass: "mainstream", wantMemGB: 6,
		},
		{
			name: "GPU with unknown VRAM → degrade to CPU-only → compact",
			hw: schema.HardwareProfile{
				Os: "linux", Arch: "amd64",
				RamGb:         ptr(32.0),
				UnifiedMemory: ptr(false),
				Gpus:          []schema.HardwareProfileGpusElem{{Name: "Mystery iGPU"}},
			},
			wantClass: "compact", wantCPUOnly: true,
		},
		{
			name: "M2 8GB unified → 5.6 usable → compact",
			hw: schema.HardwareProfile{
				Os: "darwin", Arch: "arm64",
				RamGb:         ptr(8.0),
				UnifiedMemory: ptr(true),
			},
			wantClass: "compact", wantUnified: true, wantMemGB: 5.6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Classify(tc.hw, program())
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if res.ClassID != tc.wantClass {
				t.Errorf("class = %s, want %s (detail: %s)", res.ClassID, tc.wantClass, res.Detail)
			}
			if res.CPUOnly != tc.wantCPUOnly {
				t.Errorf("cpuOnly = %v, want %v", res.CPUOnly, tc.wantCPUOnly)
			}
			if res.UnifiedMemory != tc.wantUnified {
				t.Errorf("unified = %v, want %v", res.UnifiedMemory, tc.wantUnified)
			}
			if math.Abs(res.AccelMemGB-tc.wantMemGB) > 0.01 {
				t.Errorf("accelMemGB = %.2f, want %.2f", res.AccelMemGB, tc.wantMemGB)
			}
			if res.Detail == "" || !strings.Contains(res.Detail, tc.wantClass) {
				t.Errorf("detail %q should mention class %s", res.Detail, tc.wantClass)
			}
		})
	}
}

func TestClassifyMultiGPUTakesMax(t *testing.T) {
	hw := schema.HardwareProfile{
		Os: "linux", Arch: "amd64",
		UnifiedMemory: ptr(false),
		Gpus: []schema.HardwareProfileGpusElem{
			{Name: "iGPU", VramGb: ptr(2.0)},
			{Name: "RTX 3090", VramGb: ptr(24.0)},
		},
	}
	res, err := Classify(hw, program())
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.ClassID != "ultra" || res.AccelMemGB != 24 {
		t.Fatalf("got %s/%.1f, want ultra/24", res.ClassID, res.AccelMemGB)
	}
}

func TestClassifyNoCPUOnlyClass(t *testing.T) {
	prog := schema.BenchmarkProgramV1Json{Classes: []schema.BenchmarkProgramV1JsonClassesElem{
		{Id: "only", Title: "Only", Model: "m", AccelMemMinGb: ptr(6.0)},
	}}
	if _, err := Classify(schema.HardwareProfile{Os: "linux", Arch: "amd64"}, prog); err == nil {
		t.Fatal("expected error for program without cpu_only class")
	}
}
