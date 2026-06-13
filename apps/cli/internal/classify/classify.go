// Package classify assigns a machine to a benchmark capability class from
// its detected hardware profile and a benchmark program's class table.
//
// Rules (bench-1):
//   - usable accelerator memory = the largest single GPU's VRAM
//   - Apple-Silicon unified memory counts at 70% of system RAM
//   - CPU-only machines always land in the cpu_only class regardless of RAM
//   - a GPU whose VRAM cannot be determined degrades to the CPU-only path
//     (never guess a machine into a class whose model it cannot hold)
package classify

import (
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

// unifiedMemoryFactor is the fraction of Apple-Silicon system RAM counted as
// usable accelerator memory.
const unifiedMemoryFactor = 0.7

// Result explains where the machine landed and why.
type Result struct {
	// ClassID is the program class id, e.g. "mainstream".
	ClassID string
	// AccelMemGB is the usable accelerator memory (0 for CPU-only machines).
	AccelMemGB float64
	// CPUOnly is true when the machine classified through the CPU-only path.
	CPUOnly bool
	// UnifiedMemory is true when AccelMemGB came from unified system RAM.
	UnifiedMemory bool
	// Detail is the human-readable classification rationale.
	Detail string
}

// Classify assigns hw to one of program's classes.
func Classify(hw schema.HardwareProfile, program schema.BenchmarkProgramV1Json) (Result, error) {
	res := measure(hw)
	cls, err := pick(res, program.Classes)
	if err != nil {
		return Result{}, err
	}
	res.ClassID = cls.Id
	res.Detail += fmt.Sprintf(" → class %s", cls.Id)
	return res, nil
}

// measure derives the usable accelerator memory (or CPU-only) from hardware.
func measure(hw schema.HardwareProfile) Result {
	unified := hw.UnifiedMemory != nil && *hw.UnifiedMemory

	if unified {
		if hw.RamGb != nil && *hw.RamGb > 0 {
			mem := *hw.RamGb * unifiedMemoryFactor
			return Result{
				AccelMemGB:    mem,
				UnifiedMemory: true,
				Detail: fmt.Sprintf("unified memory: %.0f GB RAM × %.0f%% = %.1f GB usable",
					*hw.RamGb, unifiedMemoryFactor*100, mem),
			}
		}
		// Unified-memory machine with unknown RAM: degrade to CPU-only.
		return Result{
			CPUOnly:       true,
			UnifiedMemory: true,
			Detail:        "unified memory but RAM size unknown — degraded to CPU-only",
		}
	}

	maxVram := 0.0
	maxName := ""
	unknownVram := false
	for _, gpu := range hw.Gpus {
		if gpu.VramGb == nil || *gpu.VramGb <= 0 {
			unknownVram = true
			continue
		}
		if *gpu.VramGb > maxVram {
			maxVram = *gpu.VramGb
			maxName = gpu.Name
		}
	}

	switch {
	case maxVram > 0:
		return Result{
			AccelMemGB: maxVram,
			Detail:     fmt.Sprintf("%s: %.1f GB VRAM usable", maxName, maxVram),
		}
	case unknownVram:
		return Result{
			CPUOnly: true,
			Detail:  "GPU detected but VRAM unknown — degraded to CPU-only",
		}
	default:
		return Result{
			CPUOnly: true,
			Detail:  "no GPU detected — CPU-only",
		}
	}
}

// pick selects the program class for the measured result: CPU-only machines
// take the cpu_only class; otherwise the first class whose
// [accel_mem_min_gb, accel_mem_max_gb) range contains the usable memory.
func pick(res Result, classes []schema.BenchmarkProgramV1JsonClassesElem) (schema.BenchmarkProgramV1JsonClassesElem, error) {
	if res.CPUOnly {
		for _, cls := range classes {
			if cls.CpuOnly != nil && *cls.CpuOnly {
				return cls, nil
			}
		}
		return schema.BenchmarkProgramV1JsonClassesElem{}, fmt.Errorf("classify: program has no cpu_only class")
	}
	for _, cls := range classes {
		min := 0.0
		if cls.AccelMemMinGb != nil {
			min = *cls.AccelMemMinGb
		}
		if res.AccelMemGB < min {
			continue
		}
		if cls.AccelMemMaxGb != nil && res.AccelMemGB >= *cls.AccelMemMaxGb {
			continue
		}
		return cls, nil
	}
	return schema.BenchmarkProgramV1JsonClassesElem{}, fmt.Errorf(
		"classify: no class covers %.1f GB of accelerator memory", res.AccelMemGB)
}
