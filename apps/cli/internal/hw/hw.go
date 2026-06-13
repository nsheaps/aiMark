// Package hw detects host hardware for the run envelope's hardware profile.
// Every probe degrades gracefully — detection failures never error a run;
// unknown fields are simply omitted or set to "unknown".
package hw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

// probeTimeout bounds each external command probe.
const probeTimeout = 3 * time.Second

// Detect builds a hardware profile for the current host. It never fails;
// fields that cannot be determined are left empty/omitted.
func Detect(ctx context.Context) schema.HardwareProfile {
	p := schema.HardwareProfile{
		Os:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	logical := runtime.NumCPU()
	p.CpuCoresLogical = &logical

	if model := cpuModel(ctx); model != "" {
		p.CpuModel = &model
	}
	if phys := physicalCores(ctx); phys > 0 {
		p.CpuCoresPhysical = &phys
	}
	if ram := ramGB(ctx); ram > 0 {
		p.RamGb = &ram
	}
	if osv := osVersion(ctx); osv != "" {
		p.OsVersion = &osv
	}
	p.Gpus = detectGPUs(ctx)

	unified := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	p.UnifiedMemory = &unified

	id := ProfileID(p)
	p.Id = &id
	return p
}

// ProfileID hashes the canonical hardware fields so identical rigs cluster.
// It is the first 16 hex chars of SHA-256 over a stable JSON encoding.
func ProfileID(p schema.HardwareProfile) string {
	canonical := map[string]any{
		"arch": p.Arch,
		"os":   p.Os,
	}
	if p.CpuModel != nil {
		canonical["cpu_model"] = *p.CpuModel
	}
	if p.CpuCoresPhysical != nil {
		canonical["cpu_cores_physical"] = *p.CpuCoresPhysical
	}
	if p.CpuCoresLogical != nil {
		canonical["cpu_cores_logical"] = *p.CpuCoresLogical
	}
	if p.RamGb != nil {
		canonical["ram_gb"] = *p.RamGb
	}
	if p.UnifiedMemory != nil {
		canonical["unified_memory"] = *p.UnifiedMemory
	}
	gpus := make([]map[string]any, 0, len(p.Gpus))
	for _, g := range p.Gpus {
		entry := map[string]any{"name": g.Name}
		if g.VramGb != nil {
			entry["vram_gb"] = *g.VramGb
		}
		if g.Vendor != nil {
			entry["vendor"] = *g.Vendor
		}
		gpus = append(gpus, entry)
	}
	canonical["gpus"] = gpus

	// json.Marshal sorts map keys, giving a stable encoding.
	raw, err := json.Marshal(canonical)
	if err != nil {
		raw = []byte(p.Os + "/" + p.Arch)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// runProbe executes an external command with a short timeout, returning
// trimmed stdout or "" on any failure.
func runProbe(ctx context.Context, name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cpuModel(ctx context.Context) string {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			return "unknown"
		}
		for _, line := range strings.Split(string(raw), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "model name", "Model", "cpu model":
				return strings.TrimSpace(value)
			}
		}
		return "unknown"
	case "darwin":
		if model := runProbe(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); model != "" {
			return model
		}
		return "unknown"
	default:
		return "unknown"
	}
}

func physicalCores(ctx context.Context) int {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			return 0
		}
		type coreKey struct{ phys, core string }
		cores := map[coreKey]bool{}
		var phys, core string
		flush := func() {
			if core != "" {
				cores[coreKey{phys, core}] = true
			}
			phys, core = "", ""
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				flush()
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "physical id":
				phys = strings.TrimSpace(value)
			case "core id":
				core = strings.TrimSpace(value)
			}
		}
		flush()
		return len(cores)
	case "darwin":
		if out := runProbe(ctx, "sysctl", "-n", "hw.physicalcpu"); out != "" {
			if n, err := strconv.Atoi(out); err == nil {
				return n
			}
		}
		return 0
	default:
		return 0
	}
}

func ramGB(ctx context.Context) float64 {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "MemTotal:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0
			}
			kb, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return 0
			}
			return roundGB(kb * 1024)
		}
		return 0
	case "darwin":
		if out := runProbe(ctx, "sysctl", "-n", "hw.memsize"); out != "" {
			if b, err := strconv.ParseFloat(out, 64); err == nil {
				return roundGB(b)
			}
		}
		return 0
	default:
		return 0
	}
}

// roundGB converts bytes to GB rounded to one decimal.
func roundGB(bytes float64) float64 {
	return math.Round(bytes/(1<<30)*10) / 10
}

func osVersion(ctx context.Context) string {
	switch runtime.GOOS {
	case "linux":
		if raw, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			return strings.TrimSpace(string(raw))
		}
		return runProbe(ctx, "uname", "-r")
	case "darwin":
		return runProbe(ctx, "sw_vers", "-productVersion")
	default:
		return ""
	}
}

func detectGPUs(ctx context.Context) []schema.HardwareProfileGpusElem {
	// NVIDIA first — works on linux and windows.
	if out := runProbe(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"); out != "" {
		var gpus []schema.HardwareProfileGpusElem
		for _, line := range strings.Split(out, "\n") {
			name, mem, ok := strings.Cut(line, ",")
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			vendor := "nvidia"
			gpu := schema.HardwareProfileGpusElem{Name: name, Vendor: &vendor}
			if ok {
				if mib, err := strconv.ParseFloat(strings.TrimSpace(mem), 64); err == nil && mib > 0 {
					vram := math.Round(mib/1024*10) / 10
					gpu.VramGb = &vram
				}
			}
			gpus = append(gpus, gpu)
		}
		if len(gpus) > 0 {
			return gpus
		}
	}

	if runtime.GOOS == "darwin" {
		if gpus := darwinGPUs(ctx); len(gpus) > 0 {
			return gpus
		}
	}

	if runtime.GOOS == "linux" {
		if gpus := lspciGPUs(ctx); len(gpus) > 0 {
			return gpus
		}
	}

	return nil
}

func darwinGPUs(ctx context.Context) []schema.HardwareProfileGpusElem {
	out := runProbe(ctx, "system_profiler", "SPDisplaysDataType", "-json")
	if out == "" {
		return nil
	}
	var parsed struct {
		SPDisplaysDataType []struct {
			Model  string `json:"sppci_model"`
			Vendor string `json:"sppci_vendor"`
			VRAM   string `json:"spdisplays_vram"`
		} `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil
	}
	var gpus []schema.HardwareProfileGpusElem
	for _, d := range parsed.SPDisplaysDataType {
		if d.Model == "" {
			continue
		}
		gpu := schema.HardwareProfileGpusElem{Name: d.Model}
		if d.Vendor != "" {
			vendor := d.Vendor
			gpu.Vendor = &vendor
		}
		// VRAM strings look like "8 GB"; discrete GPUs only.
		if fields := strings.Fields(d.VRAM); len(fields) >= 2 && strings.EqualFold(fields[1], "GB") {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				gpu.VramGb = &v
			}
		}
		gpus = append(gpus, gpu)
	}
	return gpus
}

func lspciGPUs(ctx context.Context) []schema.HardwareProfileGpusElem {
	out := runProbe(ctx, "lspci")
	if out == "" {
		return nil
	}
	var gpus []schema.HardwareProfileGpusElem
	for _, line := range strings.Split(out, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "vga compatible controller") &&
			!strings.Contains(lower, "3d controller") &&
			!strings.Contains(lower, "display controller") {
			continue
		}
		// Format: "01:00.0 VGA compatible controller: NVIDIA Corporation ..."
		_, name, ok := strings.Cut(line, "controller:")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		gpus = append(gpus, schema.HardwareProfileGpusElem{Name: name})
	}
	return gpus
}
