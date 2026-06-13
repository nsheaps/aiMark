// Package run is the benchmark engine: it executes a suite protocol against
// a target, records per-request samples, aggregates metrics, computes
// provisional scores, and builds the run.v1 envelope + samples.v1 artifact.
package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/grade"
	"github.com/nsheaps/aimark/apps/cli/internal/hw"
	"github.com/nsheaps/aimark/apps/cli/internal/integrity"
	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/score"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
	"github.com/nsheaps/aimark/apps/cli/internal/version"
)

// Options configures one benchmark run.
type Options struct {
	Suite  suites.Suite
	Target target.Target
	// Reps overrides the manifest repetitions when > 0.
	Reps int
	// Warmups overrides the manifest warmup_requests when >= 0 (-1 = manifest).
	Warmups int
	// Source is the run origin; use DetectSource to compute it.
	Source schema.RunV1JsonSource
	// Params is the user-supplied parameter vector (--param k=v).
	Params map[string]any
	// PriceIn/PriceOut are $ per Mtok for the pricing snapshot (0 = unknown).
	PriceIn  float64
	PriceOut float64
	// Progress receives one line per completed request (may be nil).
	Progress func(format string, args ...any)
	// SkipHardwareDetection disables hardware probing (tests).
	SkipHardwareDetection bool
	// SweepID, when set, marks this run as one cell of a parameter sweep.
	SweepID string
	// BenchID, when set, marks this run as one cell of a zero-choice
	// benchmark program (stamped into the envelope before signing).
	BenchID string
	// Temperature overrides the manifest decoding temperature (sweeps).
	Temperature *float64
	// MaxTokens overrides the manifest decoding max_tokens (sweeps).
	MaxTokens *int
	// Concurrency overrides the manifest concurrency_levels (sweeps).
	Concurrency []int
}

// Outcome is a completed run: the signed envelope plus raw samples.
type Outcome struct {
	Envelope  schema.RunV1Json
	Samples   schema.SamplesV1Json
	SubScores map[string]float64
	Composite float64 // 0 when not computable
}

// DetectSource resolves the run source: an explicit override wins, otherwise
// CI is detected from the CI / GITHUB_ACTIONS environment variables.
func DetectSource(override string) (schema.RunV1JsonSource, error) {
	switch override {
	case "":
		// fall through to detection
	case "user":
		return schema.RunV1JsonSourceUser, nil
	case "ci":
		return schema.RunV1JsonSourceCi, nil
	case "dev":
		return schema.RunV1JsonSourceDev, nil
	default:
		return "", fmt.Errorf("run: invalid --source %q (must be user, ci, or dev)", override)
	}
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return schema.RunV1JsonSourceCi, nil
	}
	return schema.RunV1JsonSourceUser, nil
}

func (o Options) progressf(format string, args ...any) {
	if o.Progress != nil {
		o.Progress(format, args...)
	}
}

// measured holds the timing data captured for one request.
type measured struct {
	sample       schema.SamplesV1JsonSamplesElem
	ttftMs       float64
	latencyMs    float64
	decodeTps    float64
	interTokenMs float64
	prefillTps   float64
	outputTokens int
	graded       bool
	passed       bool
}

// collector accumulates samples and per-metric series across (possibly
// concurrent) requests. All methods are safe for concurrent use.
type collector struct {
	mu       sync.Mutex
	opts     Options
	total    int
	done     int
	samples  []schema.SamplesV1JsonSamplesElem
	ttfts    []float64
	lats     []float64
	decodes  []float64
	inters   []float64
	prefills []float64
	graded   int
	passed   int
}

// add records one measured request, updates aggregates, and emits progress.
// It returns the sample's latency and output tokens for per-level rollups.
func (c *collector) add(m measured, task string, rep int, warmup bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.samples = append(c.samples, m.sample)
	c.done++

	label := ""
	if warmup {
		label = " warmup"
	}
	if m.sample.Status == schema.SamplesV1JsonSamplesElemStatusOk {
		gradeNote := ""
		if m.graded {
			if m.passed {
				gradeNote = " grade=pass"
			} else {
				gradeNote = " grade=fail"
			}
		}
		c.opts.progressf("[%d/%d]%s %s rep %d  ttft=%.0fms latency=%.0fms tps=%.1f%s",
			c.done, c.total, label, task, rep, m.ttftMs, m.latencyMs, m.decodeTps, gradeNote)
	} else {
		detail := ""
		if m.sample.Error != nil {
			detail = ": " + *m.sample.Error
		}
		c.opts.progressf("[%d/%d]%s %s rep %d  %s%s", c.done, c.total, label, task, rep, m.sample.Status, detail)
	}

	if warmup || m.sample.Status != schema.SamplesV1JsonSamplesElemStatusOk {
		return
	}
	if m.ttftMs > 0 {
		c.ttfts = append(c.ttfts, m.ttftMs)
	}
	if m.latencyMs > 0 {
		c.lats = append(c.lats, m.latencyMs)
	}
	if m.decodeTps > 0 {
		c.decodes = append(c.decodes, m.decodeTps)
	}
	if m.interTokenMs > 0 {
		c.inters = append(c.inters, m.interTokenMs)
	}
	if m.prefillTps > 0 {
		c.prefills = append(c.prefills, m.prefillTps)
	}
	if m.graded {
		c.graded++
		if m.passed {
			c.passed++
		}
	}
}

func (c *collector) okCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.samples {
		if (s.Warmup == nil || !*s.Warmup) && s.Status == schema.SamplesV1JsonSamplesElemStatusOk {
			n++
		}
	}
	return n
}

// job is one queued request in a concurrent level.
type job struct {
	task schema.SuiteManifestV1JsonTasksElem
	rep  int
}

// Execute runs the full suite protocol and returns the signed envelope and
// samples artifact (it does not write them; see results.Store).
func Execute(ctx context.Context, opts Options) (*Outcome, error) {
	manifest := opts.Suite.Manifest
	protocol := manifest.Protocol

	if opts.Temperature != nil {
		protocol.Decoding.Temperature = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		protocol.Decoding.MaxTokens = *opts.MaxTokens
	}

	reps := protocol.Repetitions
	if opts.Reps > 0 {
		reps = opts.Reps
	}
	warmups := protocol.WarmupRequests
	if opts.Warmups >= 0 {
		warmups = opts.Warmups
	}
	if len(manifest.Tasks) == 0 {
		return nil, fmt.Errorf("run: suite %s has no tasks", opts.Suite.Key)
	}

	levels := protocol.ConcurrencyLevels
	if len(opts.Concurrency) > 0 {
		levels = opts.Concurrency
	}
	for _, level := range levels {
		if level < 1 {
			return nil, fmt.Errorf("run: invalid concurrency level %d", level)
		}
	}

	info, err := opts.Target.Describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: describe target: %w", err)
	}

	timeout := time.Duration(protocol.RequestTimeoutMs) * time.Millisecond
	passes := 1
	if len(levels) > 0 {
		passes = len(levels)
	}
	col := &collector{
		opts:    opts,
		total:   warmups + reps*len(manifest.Tasks)*passes,
		samples: make([]schema.SamplesV1JsonSamplesElem, 0, warmups+reps*len(manifest.Tasks)*passes),
	}

	// Warmups run single-stream regardless of concurrency levels.
	for i := 0; i < warmups; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := manifest.Tasks[i%len(manifest.Tasks)]
		m := runRequest(ctx, opts.Target, task, protocol, timeout, 0, true)
		col.add(m, task.Id, 0, true)
	}

	levelMetrics := map[string]float64{}
	if len(levels) == 0 {
		for rep := 0; rep < reps; rep++ {
			for _, task := range manifest.Tasks {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				m := runRequest(ctx, opts.Target, task, protocol, timeout, rep, false)
				col.add(m, task.Id, rep, false)
			}
		}
	} else {
		throughputs := map[int]float64{}
		for _, level := range levels {
			throughput, p99, err := runLevel(ctx, opts, col, manifest.Tasks, protocol, timeout, reps, level)
			if err != nil {
				return nil, err
			}
			if throughput > 0 {
				levelMetrics[fmt.Sprintf("throughput_tps_c%d", level)] = throughput
				throughputs[level] = throughput
			}
			if p99 > 0 {
				levelMetrics[fmt.Sprintf("latency_ms_p99_c%d", level)] = p99
			}
		}
		minLevel, maxLevel := levels[0], levels[0]
		for _, level := range levels {
			if level < minLevel {
				minLevel = level
			}
			if level > maxLevel {
				maxLevel = level
			}
		}
		tMin, tMax := throughputs[minLevel], throughputs[maxLevel]
		if maxLevel > minLevel && tMin > 0 && tMax > 0 {
			levelMetrics["throughput_scaling"] = (tMax / tMin) / (float64(maxLevel) / float64(minLevel))
		}
	}

	if col.okCount() == 0 {
		return nil, fmt.Errorf("run: every measured request failed — check the target and try `aimark doctor`")
	}

	metrics := aggregate(col.ttfts, col.lats, col.decodes, col.inters)
	if len(col.prefills) > 0 {
		metrics["prefill_tps_mean"] = mean(col.prefills)
	}
	if col.graded > 0 {
		metrics["quality_accuracy"] = float64(col.passed) / float64(col.graded)
	}
	for k, v := range levelMetrics {
		metrics[k] = v
	}

	subScores, composite := provisionalScores(metrics, manifest.Scoring)

	runID := results.NewRunID()
	envelope := buildEnvelope(runID, opts, protocol, info, metrics, subScores, composite)
	if err := integrity.Sign(&envelope); err != nil {
		return nil, fmt.Errorf("run: sign envelope: %w", err)
	}

	return &Outcome{
		Envelope: envelope,
		Samples: schema.SamplesV1Json{
			SchemaVersion: "aimark.samples.v1",
			RunId:         runID,
			Samples:       col.samples,
		},
		SubScores: subScores,
		Composite: composite,
	}, nil
}

// runLevel executes reps×tasks at the given concurrency with a worker pool
// and returns the level's aggregate throughput (completed output tokens per
// wall-clock second) and latency p99.
func runLevel(
	ctx context.Context,
	opts Options,
	col *collector,
	tasks []schema.SuiteManifestV1JsonTasksElem,
	protocol schema.SuiteManifestV1JsonProtocol,
	timeout time.Duration,
	reps, level int,
) (throughputTps, latencyP99Ms float64, err error) {
	opts.progressf("-- concurrency %d --", level)

	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var levelLatencies []float64
	levelTokens := 0

	workers := level
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					continue // drain
				}
				m := runRequest(ctx, opts.Target, j.task, protocol, timeout, j.rep, false)
				lvl := level
				m.sample.Concurrency = &lvl
				col.add(m, j.task.Id, j.rep, false)
				if m.sample.Status == schema.SamplesV1JsonSamplesElemStatusOk {
					mu.Lock()
					if m.latencyMs > 0 {
						levelLatencies = append(levelLatencies, m.latencyMs)
					}
					levelTokens += m.outputTokens
					mu.Unlock()
				}
			}
		}()
	}

	for rep := 0; rep < reps; rep++ {
		for _, task := range tasks {
			jobs <- job{task: task, rep: rep}
		}
	}
	close(jobs)
	wg.Wait()
	wall := time.Since(start)

	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if wall > 0 && levelTokens > 0 {
		throughputTps = float64(levelTokens) / wall.Seconds()
	}
	if len(levelLatencies) > 0 {
		latencyP99Ms = percentile(levelLatencies, 0.99)
	}
	return throughputTps, latencyP99Ms, nil
}

// runRequest executes one streaming request and measures it with the
// monotonic clock (time.Since on a single start instant).
func runRequest(
	ctx context.Context,
	tgt target.Target,
	task schema.SuiteManifestV1JsonTasksElem,
	protocol schema.SuiteManifestV1JsonProtocol,
	timeout time.Duration,
	rep int,
	warmup bool,
) measured {
	system := ""
	if task.System != nil {
		system = *task.System
	}
	req := target.CompletionRequest{
		System:      system,
		Prompt:      task.Prompt,
		Temperature: protocol.Decoding.Temperature,
		MaxTokens:   protocol.Decoding.MaxTokens,
		Timeout:     timeout,
	}

	startedAt := time.Now().UTC()
	start := time.Now() // carries the monotonic reading
	var firstAt, lastAt time.Time
	tokenCount := 0

	result, err := tgt.Complete(ctx, req, func(t target.TokenEvent) {
		if tokenCount == 0 {
			firstAt = t.At
		}
		lastAt = t.At
		tokenCount++
	})
	elapsed := time.Since(start)

	w := warmup
	elem := schema.SamplesV1JsonSamplesElem{
		TaskId:     task.Id,
		Repetition: rep,
		Warmup:     &w,
		StartedAt:  startedAt,
		Status:     schema.SamplesV1JsonSamplesElemStatusOk,
	}
	m := measured{sample: elem}

	if err != nil {
		msg := err.Error()
		m.sample.Status = schema.SamplesV1JsonSamplesElemStatusError
		if errors.Is(err, context.DeadlineExceeded) {
			m.sample.Status = schema.SamplesV1JsonSamplesElemStatusTimeout
		}
		m.sample.Error = &msg
		return m
	}

	m.latencyMs = float64(elapsed) / float64(time.Millisecond)
	m.sample.LatencyMs = &m.latencyMs

	if tokenCount > 0 {
		m.ttftMs = float64(firstAt.Sub(start)) / float64(time.Millisecond)
		m.sample.TtftMs = &m.ttftMs
	}

	outputTokens := result.OutputTokens
	if outputTokens == 0 {
		outputTokens = tokenCount
	}
	if outputTokens > 0 {
		m.sample.OutputTokens = &outputTokens
		m.outputTokens = outputTokens
	}
	if result.InputTokens > 0 {
		in := result.InputTokens
		m.sample.InputTokens = &in
		// Prefill rate: prompt tokens processed before the first output token.
		if m.ttftMs > 0 {
			m.prefillTps = float64(in) / (m.ttftMs / 1000)
		}
	}

	// Decode rate and inter-token gap over the streamed window (first to
	// last token, monotonic clock).
	if tokenCount >= 2 {
		window := lastAt.Sub(firstAt)
		if window > 0 {
			m.decodeTps = float64(outputTokens-1) / window.Seconds()
			m.sample.DecodeTps = &m.decodeTps
			m.interTokenMs = (float64(window) / float64(time.Millisecond)) / float64(tokenCount-1)
			m.sample.InterTokenMsMean = &m.interTokenMs
		}
	}

	// Objective grading (quality suites).
	if task.Grading != nil {
		res := grade.Grade(task.Grading, result.Text)
		passed := res.Passed
		detail := res.Detail
		m.sample.Grade = &schema.SamplesV1JsonSamplesElemGrade{Passed: &passed, Detail: &detail}
		m.graded = true
		m.passed = passed
	}

	return m
}

// provisionalScores computes every sub-score whose metrics are all present,
// then the composite over those. A sub-score with any missing metric is
// skipped, mirroring how the server treats absent sub-scores.
func provisionalScores(metrics map[string]float64, scoring schema.SuiteManifestV1JsonScoring) (map[string]float64, float64) {
	specsByName := map[string]*schema.SubScoreSpec{
		"performance":     scoring.Performance,
		"quality":         scoring.Quality,
		"cost_efficiency": scoring.CostEfficiency,
		"consistency":     scoring.Consistency,
	}
	subScores := map[string]float64{}
	for name, spec := range specsByName {
		if spec == nil {
			continue
		}
		converted := make([]score.MetricSpec, 0, len(spec.Metrics))
		for _, m := range spec.Metrics {
			converted = append(converted, score.MetricSpec{
				Key:           m.Key,
				Reference:     m.Reference,
				Weight:        m.Weight,
				LowerIsBetter: m.LowerIsBetter != nil && *m.LowerIsBetter,
			})
		}
		value, err := score.SubScore(metrics, converted)
		if err != nil {
			continue // missing metric — skip this sub-score
		}
		subScores[name] = value
	}
	composite, err := score.Composite(subScores, scoring.CompositeWeights)
	if err != nil {
		composite = 0
	}
	return subScores, composite
}

func buildEnvelope(
	runID string,
	opts Options,
	protocol schema.SuiteManifestV1JsonProtocol,
	info target.TargetInfo,
	metrics map[string]float64,
	subScores map[string]float64,
	composite float64,
) schema.RunV1Json {
	manifest := opts.Suite.Manifest
	protocolHash := opts.Suite.ProtocolHash()

	params := schema.RunV1JsonTargetParams{
		"temperature": protocol.Decoding.Temperature,
		"max_tokens":  protocol.Decoding.MaxTokens,
	}
	for k, v := range opts.Params {
		params[k] = v
	}

	tgt := schema.RunV1JsonTarget{
		Kind:   schema.RunV1JsonTargetKind(info.Kind),
		Model:  info.Model,
		Params: params,
	}
	if info.Runtime != "" {
		tgt.Runtime = &info.Runtime
	}
	if info.RuntimeVersion != "" {
		tgt.RuntimeVersion = &info.RuntimeVersion
	}
	if info.Provider != "" {
		tgt.Provider = &info.Provider
	}
	if info.ModelDigest != "" {
		tgt.ModelDigest = &info.ModelDigest
	}
	if info.Quantization != "" {
		tgt.Quantization = &info.Quantization
	}
	if info.DeclaredParamsB > 0 {
		tgt.DeclaredParamsB = &info.DeclaredParamsB
	}

	envelope := schema.RunV1Json{
		SchemaVersion: "aimark.run.v1",
		RunId:         runID,
		CreatedAt:     time.Now().UTC(),
		Source:        opts.Source,
		Cli: schema.RunV1JsonCli{
			Version: version.Version,
			Os:      schema.RunV1JsonCliOs(runtime.GOOS),
			Arch:    schema.RunV1JsonCliArch(runtime.GOARCH),
		},
		Suite: schema.RunV1JsonSuite{
			Id:           manifest.Id,
			Version:      manifest.Version,
			ProtocolHash: &protocolHash,
		},
		Target:  tgt,
		Metrics: metrics,
	}
	if opts.SweepID != "" {
		sweepID := opts.SweepID
		envelope.SweepId = &sweepID
	}
	if opts.BenchID != "" {
		benchID := opts.BenchID
		envelope.BenchId = &benchID
	}
	if version.Commit != "" && version.Commit != "none" {
		commit := version.Commit
		envelope.Cli.Commit = &commit
	}

	if !opts.SkipHardwareDetection {
		profile := hw.Detect(context.Background())
		envelope.Environment = &schema.RunV1JsonEnvironment{HardwareProfile: &profile}
	}

	if len(subScores) > 0 {
		scores := &schema.RunV1JsonProvisionalScores{}
		setIf := func(dst **float64, key string) {
			if v, ok := subScores[key]; ok {
				val := v
				*dst = &val
			}
		}
		setIf(&scores.Performance, "performance")
		setIf(&scores.Quality, "quality")
		setIf(&scores.CostEfficiency, "cost_efficiency")
		setIf(&scores.Consistency, "consistency")
		if composite > 0 {
			c := composite
			scores.Composite = &c
		}
		envelope.ProvisionalScores = scores
	}

	if opts.PriceIn > 0 || opts.PriceOut > 0 {
		now := time.Now().UTC()
		currency := "USD"
		snapshot := &schema.RunV1JsonPricingSnapshot{
			Currency:   &currency,
			CapturedAt: &now,
		}
		if opts.PriceIn > 0 {
			in := opts.PriceIn
			snapshot.InputPerMtok = &in
		}
		if opts.PriceOut > 0 {
			out := opts.PriceOut
			snapshot.OutputPerMtok = &out
		}
		envelope.PricingSnapshot = snapshot
	}

	return envelope
}
