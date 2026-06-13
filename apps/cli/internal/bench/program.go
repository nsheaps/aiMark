// Package bench is the zero-choice benchmark conductor: it classifies the
// machine, resolves the frozen program's cells for that class, fetches the
// pinned runtime + model assets, drives the suite engine through the managed
// local server, and assembles the signed benchmark.v1 envelope.
//
// The files under embedded/ are byte-for-byte copies of
// packages/suites/programs/<id>-<version>/program.json, synced by the
// "codegen:suites" script in apps/cli/package.json. A test
// (TestEmbeddedProgramsMatchSource) fails when the copies drift.
package bench

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
)

//go:embed embedded/*.json
var embeddedFS embed.FS

// DefaultProgram is the program key bare `aimark` runs.
const DefaultProgram = "bench-1"

// Program is one embedded benchmark program plus its raw frozen bytes.
type Program struct {
	// Key is the canonical "<id>-<version>" identifier, e.g. "bench-1".
	Key string
	// Manifest is the parsed program.
	Manifest schema.BenchmarkProgramV1Json
	// Raw is the exact embedded program bytes.
	Raw []byte
}

var (
	loadOnce sync.Once
	loaded   []Program
	loadErr  error
)

func load() ([]Program, error) {
	loadOnce.Do(func() {
		entries, err := embeddedFS.ReadDir("embedded")
		if err != nil {
			loadErr = fmt.Errorf("bench: read embedded dir: %w", err)
			return
		}
		for _, entry := range entries {
			raw, err := embeddedFS.ReadFile("embedded/" + entry.Name())
			if err != nil {
				loadErr = fmt.Errorf("bench: read %s: %w", entry.Name(), err)
				return
			}
			var manifest schema.BenchmarkProgramV1Json
			if err := json.Unmarshal(raw, &manifest); err != nil {
				loadErr = fmt.Errorf("bench: parse %s: %w", entry.Name(), err)
				return
			}
			key := fmt.Sprintf("%s-%d", manifest.Id, manifest.Version)
			if want := key + ".json"; entry.Name() != want {
				loadErr = fmt.Errorf("bench: embedded file %s should be named %s", entry.Name(), want)
				return
			}
			loaded = append(loaded, Program{Key: key, Manifest: manifest, Raw: raw})
		}
		sort.Slice(loaded, func(i, j int) bool { return loaded[i].Key < loaded[j].Key })
	})
	return loaded, loadErr
}

// All returns every embedded program, sorted by key.
func All() ([]Program, error) {
	return load()
}

// Get resolves a program by "<id>-<version>" key.
func Get(key string) (Program, error) {
	all, err := load()
	if err != nil {
		return Program{}, err
	}
	for _, p := range all {
		if p.Key == key {
			return p, nil
		}
	}
	keys := make([]string, 0, len(all))
	for _, p := range all {
		keys = append(keys, p.Key)
	}
	return Program{}, fmt.Errorf("bench: unknown program %q (available: %s)", key, strings.Join(keys, ", "))
}

// ResolvedCell is one program cell bound to a class: suite loaded, model
// reference resolved to a concrete program model.
type ResolvedCell struct {
	// ID is the cell id from the manifest, e.g. "sprint-class".
	ID string
	// Suite is the loaded suite this cell runs.
	Suite suites.Suite
	// Model is the resolved program model (models[] entry).
	Model schema.BenchmarkProgramV1JsonModelsElem
	// Role is "score" or "validity".
	Role schema.BenchmarkProgramV1JsonCellsElemRole
	// Weight is the cell's composite weight (score cells; 0 for validity).
	Weight float64
	// RepsOverride caps repetitions when > 0.
	RepsOverride int
	// Params is the cell's recorded parameter vector from the manifest.
	Params map[string]any
	// ValidityMinAccuracy flags the benchmark when quality_accuracy falls
	// below it (validity cells; nil otherwise).
	ValidityMinAccuracy *float64
}

// classByID finds a program class.
func classByID(p schema.BenchmarkProgramV1Json, id string) (schema.BenchmarkProgramV1JsonClassesElem, error) {
	for _, cls := range p.Classes {
		if cls.Id == id {
			return cls, nil
		}
	}
	return schema.BenchmarkProgramV1JsonClassesElem{}, fmt.Errorf("bench: program has no class %q", id)
}

// modelByID finds a program model asset.
func modelByID(p schema.BenchmarkProgramV1Json, id string) (schema.BenchmarkProgramV1JsonModelsElem, error) {
	for _, m := range p.Models {
		if m.Id == id {
			return m, nil
		}
	}
	return schema.BenchmarkProgramV1JsonModelsElem{}, fmt.Errorf("bench: program references unknown model %q", id)
}

// ResolveCells returns the concrete cells the given class runs, in manifest
// order. Anchor cells that resolve to the class model are dropped (the class
// model IS the anchor — never run the same cell twice).
func ResolveCells(p schema.BenchmarkProgramV1Json, classID string) ([]ResolvedCell, error) {
	cls, err := classByID(p, classID)
	if err != nil {
		return nil, err
	}

	var cells []ResolvedCell
	for _, cell := range p.Cells {
		if cell.Class != "*" && cell.Class != classID {
			continue
		}

		modelID := ""
		switch cell.Model {
		case schema.BenchmarkProgramV1JsonCellsElemModelClass:
			modelID = cls.Model
		case schema.BenchmarkProgramV1JsonCellsElemModelAnchor:
			if p.AnchorModel == nil {
				return nil, fmt.Errorf("bench: cell %s references anchor model but program has none", cell.Id)
			}
			modelID = *p.AnchorModel
			if modelID == cls.Model {
				continue // the class model is the anchor — skip the duplicate cell
			}
		}
		model, err := modelByID(p, modelID)
		if err != nil {
			return nil, fmt.Errorf("bench: cell %s: %w", cell.Id, err)
		}

		suiteKey := fmt.Sprintf("%s-%d", cell.Suite, cell.SuiteVersion)
		suite, err := suites.Get(suiteKey)
		if err != nil {
			return nil, fmt.Errorf("bench: cell %s: %w", cell.Id, err)
		}

		resolved := ResolvedCell{
			ID:    cell.Id,
			Suite: suite,
			Model: model,
			Role:  cell.Role,
		}
		if cell.Weight != nil {
			resolved.Weight = *cell.Weight
		}
		if cell.RepsOverride != nil {
			resolved.RepsOverride = *cell.RepsOverride
		}
		if len(cell.Params) > 0 {
			resolved.Params = map[string]any{}
			for k, v := range cell.Params {
				resolved.Params[k] = v
			}
		}
		if cell.ValidityMinAccuracy != nil {
			v := *cell.ValidityMinAccuracy
			resolved.ValidityMinAccuracy = &v
		}
		cells = append(cells, resolved)
	}
	if len(cells) == 0 {
		return nil, fmt.Errorf("bench: program %s-%d has no cells for class %s", p.Id, p.Version, classID)
	}
	return cells, nil
}

// RuntimeAssetFor picks the runtime asset for (os, arch), preferring the
// best accelerator the machine can use: cpuOnly machines take the cpu build;
// everything else takes metal > cuda > vulkan > cpu in that order.
func RuntimeAssetFor(p schema.BenchmarkProgramV1Json, osName, arch string, cpuOnly bool) (schema.BenchmarkProgramV1JsonRuntimeAssetsElem, error) {
	var candidates []schema.BenchmarkProgramV1JsonRuntimeAssetsElem
	for _, a := range p.Runtime.Assets {
		if string(a.Os) == osName && string(a.Arch) == arch {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return schema.BenchmarkProgramV1JsonRuntimeAssetsElem{},
			fmt.Errorf("bench: no %s runtime build for %s/%s in program %s-%d", p.Runtime.Engine, osName, arch, p.Id, p.Version)
	}

	rank := func(accel schema.BenchmarkProgramV1JsonRuntimeAssetsElemAccel) int {
		if cpuOnly {
			if accel == schema.BenchmarkProgramV1JsonRuntimeAssetsElemAccelCpu {
				return 0
			}
			return 100
		}
		switch accel {
		case schema.BenchmarkProgramV1JsonRuntimeAssetsElemAccelMetal:
			return 0
		case schema.BenchmarkProgramV1JsonRuntimeAssetsElemAccelCuda:
			return 1
		case schema.BenchmarkProgramV1JsonRuntimeAssetsElemAccelVulkan:
			return 2
		default:
			return 3
		}
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if rank(c.Accel) < rank(best.Accel) {
			best = c
		}
	}
	return best, nil
}
