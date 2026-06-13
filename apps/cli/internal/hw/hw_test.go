package hw

import (
	"context"
	"regexp"
	"runtime"
	"testing"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
)

func TestDetectNeverFails(t *testing.T) {
	p := Detect(context.Background())
	if p.Os != runtime.GOOS {
		t.Errorf("Os = %q, want %q", p.Os, runtime.GOOS)
	}
	if p.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", p.Arch, runtime.GOARCH)
	}
	if p.CpuCoresLogical == nil || *p.CpuCoresLogical < 1 {
		t.Error("CpuCoresLogical missing")
	}
	if p.Id == nil {
		t.Fatal("Id missing")
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(*p.Id) {
		t.Errorf("Id = %q, want 16 lowercase hex chars", *p.Id)
	}
	if p.UnifiedMemory == nil {
		t.Error("UnifiedMemory missing")
	}
}

func TestProfileIDStable(t *testing.T) {
	model := "TestCPU"
	cores := 8
	ram := 32.0
	unified := false
	vram := 24.0
	vendor := "nvidia"
	build := func() schema.HardwareProfile {
		return schema.HardwareProfile{
			Os:               "linux",
			Arch:             "amd64",
			CpuModel:         &model,
			CpuCoresPhysical: &cores,
			RamGb:            &ram,
			UnifiedMemory:    &unified,
			Gpus: []schema.HardwareProfileGpusElem{
				{Name: "RTX 4090", VramGb: &vram, Vendor: &vendor},
			},
		}
	}
	a := ProfileID(build())
	b := ProfileID(build())
	if a != b {
		t.Fatalf("ProfileID not stable: %q != %q", a, b)
	}

	other := build()
	otherModel := "OtherCPU"
	other.CpuModel = &otherModel
	if ProfileID(other) == a {
		t.Fatal("ProfileID should change when canonical fields change")
	}

	// Id field itself must not feed back into the hash.
	withID := build()
	id := "deadbeefdeadbeef"
	withID.Id = &id
	if ProfileID(withID) != a {
		t.Fatal("ProfileID must ignore the Id field")
	}
}

func TestRoundGB(t *testing.T) {
	if got := roundGB(16 * (1 << 30)); got != 16 {
		t.Errorf("roundGB(16GiB) = %v", got)
	}
	if got := roundGB(8.25 * (1 << 30)); got != 8.3 {
		t.Errorf("roundGB(8.25GiB) = %v", got)
	}
}
