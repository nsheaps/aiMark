package bench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/assets"
	"github.com/nsheaps/aimark/apps/cli/internal/classify"
	"github.com/nsheaps/aimark/apps/cli/internal/engine"
	"github.com/nsheaps/aimark/apps/cli/internal/hw"
	"github.com/nsheaps/aimark/apps/cli/internal/integrity"
	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
	"github.com/nsheaps/aimark/apps/cli/internal/version"
)

// Environment variables for e2e/dev use only (documented in `aimark bench
// --help`):
const (
	// EnvRuntimeURL points cells at an existing OpenAI-compatible server,
	// bypassing asset download and engine management (classification still
	// runs; program model ids are still recorded).
	EnvRuntimeURL = "AIMARK_BENCH_RUNTIME_URL"
	// EnvFast trims the program for smoke tests: reps capped at 1 and
	// marathon concurrency levels reduced to [1, 2].
	EnvFast = "AIMARK_BENCH_FAST"
)

// Options configures one zero-choice benchmark execution.
type Options struct {
	// Program is the frozen program to run (Get(DefaultProgram) normally).
	Program Program
	// Source is the benchmark origin (auto-detected by the command layer).
	Source schema.RunV1JsonSource
	// ClassOverride forces a class id (debug; the command layer marks the
	// source dev so overridden runs never pollute user boards).
	ClassOverride string
	// HardwareProfile overrides detection (tests). nil = detect.
	HardwareProfile *schema.HardwareProfile
	// Progress receives human-readable status lines (may be nil).
	Progress func(format string, args ...any)
	// ConfirmDownload is consulted before downloading assets (real path
	// only). nil = proceed. Return false to abort.
	ConfirmDownload func(plan DownloadPlan) bool
}

// DownloadPlan lists the assets a benchmark needs before it can run.
type DownloadPlan struct {
	// Items are the assets in download order (runtime first).
	Items []DownloadItem
	// TotalBytes sums the expected sizes of items not yet cached.
	TotalBytes int64
}

// DownloadItem is one asset in the plan.
type DownloadItem struct {
	Label     string
	SizeBytes int64
	Cached    bool
}

// CellOutcome is one executed cell.
type CellOutcome struct {
	Cell ResolvedCell
	// Outcome is the completed suite run.
	Outcome *run.Outcome
	// ValidityFailed is true for validity cells below their accuracy floor.
	ValidityFailed bool
}

// Outcome is one completed zero-choice benchmark.
type Outcome struct {
	Envelope       schema.BenchmarkV1Json
	Cells          []CellOutcome
	Classification classify.Result
	// ValidityFailed is true when any validity cell failed; the benchmark is
	// still saved and uploadable — the server decides how to treat it.
	ValidityFailed bool
	Performance    float64
	Consistency    float64
	Composite      float64
}

// Execute runs the program end to end and returns the signed benchmark
// envelope plus per-cell outcomes. The caller persists results.
func Execute(ctx context.Context, opts Options) (*Outcome, error) {
	progress := func(format string, args ...any) {
		if opts.Progress != nil {
			opts.Progress(format, args...)
		}
	}
	manifest := opts.Program.Manifest

	// 1. Detect hardware and classify.
	var profile schema.HardwareProfile
	if opts.HardwareProfile != nil {
		profile = *opts.HardwareProfile
	} else {
		profile = hw.Detect(ctx)
	}
	cls, err := classify.Classify(profile, manifest)
	if err != nil {
		return nil, err
	}
	if opts.ClassOverride != "" {
		if _, err := classByID(manifest, opts.ClassOverride); err != nil {
			return nil, err
		}
		cls.Detail = fmt.Sprintf("class forced to %s by --class (measured: %s)", opts.ClassOverride, cls.Detail)
		cls.ClassID = opts.ClassOverride
	}
	progress("classification: %s", cls.Detail)

	cells, err := ResolveCells(manifest, cls.ClassID)
	if err != nil {
		return nil, err
	}
	class, err := classByID(manifest, cls.ClassID)
	if err != nil {
		return nil, err
	}
	contextBudget := 0
	if class.ContextBudget != nil {
		contextBudget = *class.ContextBudget
	}

	fast := os.Getenv(EnvFast) != ""
	if fast {
		progress("%s set: reps capped at 1, marathon trimmed to concurrency [1 2]", EnvFast)
	}

	// 2. Resolve how cells reach a server: managed engine or external URL.
	mockURL := os.Getenv(EnvRuntimeURL)
	var runtimeBin string
	if mockURL == "" {
		runtimeBin, err = ensureAssets(opts, cells, cls, progress)
		if err != nil {
			return nil, err
		}
	} else {
		progress("%s set: using external runtime at %s (assets + engine skipped)", EnvRuntimeURL, mockURL)
	}

	// 3. Run cells grouped by model — one server per model.
	benchID := results.NewRunID()
	progress("bench %s: class %s, %d cells", benchID, cls.ClassID, len(cells))

	outcomes := make([]CellOutcome, 0, len(cells))
	for _, group := range groupByModel(cells) {
		baseURL := mockURL
		var srv *engine.Server
		if mockURL == "" {
			modelPath, err := modelAssetPath(group.model)
			if err != nil {
				return nil, err
			}
			srv, err = engine.Start(ctx, engine.Options{
				BinPath:    runtimeBin,
				ModelPath:  modelPath,
				ModelAlias: group.model.Id,
				CtxSize:    contextBudget,
				Log:        opts.Progress,
				LogFile:    serverLogPath(),
			})
			if err != nil {
				return nil, fmt.Errorf("bench: start server for %s: %w", group.model.Id, err)
			}
			baseURL = srv.BaseURL()
		}

		for _, cell := range group.cells {
			outcome, err := executeCell(ctx, opts, cell, baseURL, benchID, contextBudget, cls, fast, progress)
			if err != nil {
				if srv != nil {
					srv.Stop()
				}
				return nil, err
			}
			outcomes = append(outcomes, *outcome)
		}
		if srv != nil {
			srv.Stop()
		}
	}

	// 4. Provisional benchmark scores + envelope.
	out := buildOutcome(benchID, opts, profile, cls, outcomes)
	if err := integrity.SignBenchmark(&out.Envelope); err != nil {
		return nil, fmt.Errorf("bench: sign envelope: %w", err)
	}
	return out, nil
}

// modelGroup is the consecutive cells sharing one model.
type modelGroup struct {
	model schema.BenchmarkProgramV1JsonModelsElem
	cells []ResolvedCell
}

// groupByModel batches cells per model (order of first appearance) so each
// model's server starts exactly once.
func groupByModel(cells []ResolvedCell) []modelGroup {
	var groups []modelGroup
	index := map[string]int{}
	for _, cell := range cells {
		i, ok := index[cell.Model.Id]
		if !ok {
			i = len(groups)
			index[cell.Model.Id] = i
			groups = append(groups, modelGroup{model: cell.Model})
		}
		groups[i].cells = append(groups[i].cells, cell)
	}
	return groups
}

// ensureAssets prints the download plan, asks for confirmation, and makes
// the runtime + model assets available. It returns the server binary path.
func ensureAssets(opts Options, cells []ResolvedCell, cls classify.Result, progress func(string, ...any)) (string, error) {
	manifest := opts.Program.Manifest
	runtimeAsset, err := RuntimeAssetFor(manifest, runtime.GOOS, runtime.GOARCH, cls.CPUOnly)
	if err != nil {
		return "", err
	}
	if runtimeAsset.ServerPath == nil || *runtimeAsset.ServerPath == "" {
		return "", fmt.Errorf("bench: runtime asset %s has no server_path", runtimeAsset.Url)
	}

	mgr, err := assets.Open(opts.Progress)
	if err != nil {
		return "", err
	}

	// Build the plan: runtime first, then each distinct model.
	runtimeKey := fmt.Sprintf("%s-%s-%s-%s", manifest.Runtime.Build, runtimeAsset.Os, runtimeAsset.Arch, runtimeAsset.Accel)
	plan := DownloadPlan{}
	addItem := func(label string, a assets.Asset, cached bool) {
		size := a.SizeBytes
		plan.Items = append(plan.Items, DownloadItem{Label: label, SizeBytes: size, Cached: cached})
		if !cached {
			plan.TotalBytes += size
		}
	}
	runtimeDl := toAsset(runtimeAsset.Url, runtimeAsset.Sha256, runtimeAsset.SizeBytes)
	addItem(fmt.Sprintf("%s %s (%s/%s/%s)", manifest.Runtime.Engine, manifest.Runtime.Build, runtimeAsset.Os, runtimeAsset.Arch, runtimeAsset.Accel),
		runtimeDl, isCached(mgr, runtimeDl))
	for _, group := range groupByModel(cells) {
		dl := toAsset(group.model.Url, &group.model.Sha256, group.model.SizeBytes)
		addItem(fmt.Sprintf("model %s", group.model.Id), dl, isCached(mgr, dl))
	}

	progress("download plan:")
	for _, item := range plan.Items {
		state := ""
		if item.Cached {
			state = " (cached)"
		}
		progress("  %-52s %10s%s", item.Label, assets.HumanBytes(item.SizeBytes), state)
	}
	progress("  total to download: %s", assets.HumanBytes(plan.TotalBytes))

	if opts.ConfirmDownload != nil && !opts.ConfirmDownload(plan) {
		return "", fmt.Errorf("bench: aborted before download")
	}

	runtimeDir, err := mgr.EnsureExtracted(runtimeDl, runtimeKey)
	if err != nil {
		return "", err
	}
	serverBin := filepath.Join(runtimeDir, filepath.FromSlash(*runtimeAsset.ServerPath))
	if _, err := os.Stat(serverBin); err != nil {
		return "", fmt.Errorf("bench: server binary missing after extraction: %s", serverBin)
	}

	for _, group := range groupByModel(cells) {
		if _, err := mgr.Ensure(toAsset(group.model.Url, &group.model.Sha256, group.model.SizeBytes)); err != nil {
			return "", err
		}
	}
	return serverBin, nil
}

// toAsset converts manifest url/sha/size fields into a download asset.
func toAsset(url string, sha *string, size *int) assets.Asset {
	a := assets.Asset{URL: url}
	if sha != nil {
		a.Sha256 = *sha
	}
	if size != nil {
		a.SizeBytes = int64(*size)
	}
	return a
}

// isCached reports whether the asset is already verified locally.
func isCached(mgr *assets.Manager, a assets.Asset) bool {
	name, err := a.Name()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(mgr.Dir, name+".ok"))
	if err != nil {
		return false
	}
	return a.Sha256 == "" || string(raw) == a.Sha256+"\n"
}

// modelAssetPath returns the cached path of a model GGUF (already ensured).
func modelAssetPath(model schema.BenchmarkProgramV1JsonModelsElem) (string, error) {
	mgr, err := assets.Open(nil)
	if err != nil {
		return "", err
	}
	name, err := (assets.Asset{URL: model.Url}).Name()
	if err != nil {
		return "", err
	}
	return filepath.Join(mgr.Dir, name), nil
}

// serverLogPath places the engine log next to the results.
func serverLogPath() string {
	dir, err := results.DefaultDir()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(dir), "llama-server.log")
}

// executeCell runs one cell through the suite engine.
func executeCell(
	ctx context.Context,
	opts Options,
	cell ResolvedCell,
	baseURL, benchID string,
	contextBudget int,
	cls classify.Result,
	fast bool,
	progress func(string, ...any),
) (*CellOutcome, error) {
	progress("-- cell %s: %s on %s --", cell.ID, cell.Suite.Key, cell.Model.Id)

	tgt := target.NewOpenAI(baseURL, cell.Model.Id, os.Getenv("AIMARK_TARGET_API_KEY"))

	params := map[string]any{
		"bench_cell": cell.ID,
		"class":      cls.ClassID,
	}
	if contextBudget > 0 {
		params["num_ctx"] = contextBudget
	}
	for k, v := range cell.Params {
		params[k] = v
	}

	runOpts := run.Options{
		Suite:    cell.Suite,
		Target:   tgt,
		Source:   opts.Source,
		Params:   params,
		BenchID:  benchID,
		Progress: opts.Progress,
	}
	if cell.RepsOverride > 0 {
		runOpts.Reps = cell.RepsOverride
	}
	if fast {
		runOpts.Reps = 1
		runOpts.Warmups = 0
		if len(cell.Suite.Manifest.Protocol.ConcurrencyLevels) > 0 {
			runOpts.Concurrency = []int{1, 2}
		}
	} else {
		runOpts.Warmups = -1
	}

	outcome, err := run.Execute(ctx, runOpts)
	if err != nil {
		return nil, fmt.Errorf("bench: cell %s: %w", cell.ID, err)
	}

	co := &CellOutcome{Cell: cell, Outcome: outcome}
	if cell.Role == schema.BenchmarkProgramV1JsonCellsElemRoleValidity && cell.ValidityMinAccuracy != nil {
		accuracy, ok := outcome.Envelope.Metrics["quality_accuracy"]
		if !ok || accuracy < *cell.ValidityMinAccuracy {
			co.ValidityFailed = true
			progress("WARNING: validity cell %s below accuracy floor (%.2f < %.2f) — this benchmark will be flagged",
				cell.ID, accuracy, *cell.ValidityMinAccuracy)
		}
	}
	return co, nil
}

// buildOutcome computes the provisional benchmark scores and assembles the
// (unsigned) envelope.
func buildOutcome(
	benchID string,
	opts Options,
	profile schema.HardwareProfile,
	cls classify.Result,
	cellOutcomes []CellOutcome,
) *Outcome {
	manifest := opts.Program.Manifest

	composite := weightedGeomean(cellOutcomes, func(o CellOutcome) float64 { return o.Outcome.Composite })
	performance := weightedGeomean(cellOutcomes, func(o CellOutcome) float64 { return o.Outcome.SubScores["performance"] })
	consistency := weightedGeomean(cellOutcomes, func(o CellOutcome) float64 { return o.Outcome.SubScores["consistency"] })

	validityFailed := false
	envCells := make([]schema.BenchmarkV1JsonCellsElem, 0, len(cellOutcomes))
	for _, o := range cellOutcomes {
		envCells = append(envCells, schema.BenchmarkV1JsonCellsElem{
			CellId: o.Cell.ID,
			RunId:  o.Outcome.Envelope.RunId,
			Role:   schema.BenchmarkV1JsonCellsElemRole(o.Cell.Role),
		})
		if o.ValidityFailed {
			validityFailed = true
		}
	}

	envelope := schema.BenchmarkV1Json{
		SchemaVersion: "aimark.benchmark.v1",
		BenchId:       benchID,
		CreatedAt:     time.Now().UTC(),
		Source:        schema.BenchmarkV1JsonSource(opts.Source),
		Cli: schema.BenchmarkV1JsonCli{
			Version: version.Version,
			Os:      schema.BenchmarkV1JsonCliOs(runtime.GOOS),
			Arch:    schema.BenchmarkV1JsonCliArch(runtime.GOARCH),
		},
		Program: schema.BenchmarkV1JsonProgram{Id: manifest.Id, Version: manifest.Version},
		Class:   cls.ClassID,
		Classification: &schema.BenchmarkV1JsonClassification{
			AccelMemGb:    &cls.AccelMemGB,
			CpuOnly:       &cls.CPUOnly,
			UnifiedMemory: &cls.UnifiedMemory,
			Detail:        &cls.Detail,
		},
		Environment: &schema.BenchmarkV1JsonEnvironment{HardwareProfile: &profile},
		Cells:       envCells,
	}
	if version.Commit != "" && version.Commit != "none" {
		commit := version.Commit
		envelope.Cli.Commit = &commit
	}

	scores := &schema.BenchmarkV1JsonProvisionalScores{}
	if composite > 0 {
		c := composite
		scores.Composite = &c
	}
	if performance > 0 {
		p := performance
		scores.Performance = &p
	}
	if consistency > 0 {
		c := consistency
		scores.Consistency = &c
	}
	if scores.Composite != nil || scores.Performance != nil || scores.Consistency != nil {
		envelope.ProvisionalScores = scores
	}

	return &Outcome{
		Envelope:       envelope,
		Cells:          cellOutcomes,
		Classification: cls,
		ValidityFailed: validityFailed,
		Performance:    performance,
		Consistency:    consistency,
		Composite:      composite,
	}
}

// weightedGeomean folds the score-role cells' values (via extract) into a
// weighted geometric mean. Cells whose value is absent (<= 0) are skipped
// and their weights renormalized, mirroring score.Composite semantics.
func weightedGeomean(cells []CellOutcome, extract func(CellOutcome) float64) float64 {
	totalWeight := 0.0
	for _, c := range cells {
		if c.Cell.Role != schema.BenchmarkProgramV1JsonCellsElemRoleScore || c.Cell.Weight <= 0 {
			continue
		}
		if extract(c) > 0 {
			totalWeight += c.Cell.Weight
		}
	}
	if totalWeight <= 0 {
		return 0
	}
	logSum := 0.0
	for _, c := range cells {
		if c.Cell.Role != schema.BenchmarkProgramV1JsonCellsElemRoleScore || c.Cell.Weight <= 0 {
			continue
		}
		v := extract(c)
		if v <= 0 {
			continue
		}
		logSum += (c.Cell.Weight / totalWeight) * math.Log(v)
	}
	return math.Exp(logSum)
}
