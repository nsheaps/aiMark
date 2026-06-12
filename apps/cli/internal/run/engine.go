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
	"time"

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
}

// Execute runs the full suite protocol and returns the signed envelope and
// samples artifact (it does not write them; see results.Store).
func Execute(ctx context.Context, opts Options) (*Outcome, error) {
	manifest := opts.Suite.Manifest
	protocol := manifest.Protocol

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

	info, err := opts.Target.Describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("run: describe target: %w", err)
	}

	timeout := time.Duration(protocol.RequestTimeoutMs) * time.Millisecond
	total := warmups + reps*len(manifest.Tasks)
	done := 0

	samplesOut := make([]schema.SamplesV1JsonSamplesElem, 0, total)
	var ttfts, latencies, decodes, interTokens []float64

	execute := func(task schema.SuiteManifestV1JsonTasksElem, rep int, warmup bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		m := runRequest(ctx, opts.Target, task, protocol, timeout, rep, warmup)
		samplesOut = append(samplesOut, m.sample)
		done++
		label := ""
		if warmup {
			label = " warmup"
		}
		if m.sample.Status == schema.SamplesV1JsonSamplesElemStatusOk {
			opts.progressf("[%d/%d]%s %s rep %d  ttft=%.0fms latency=%.0fms tps=%.1f",
				done, total, label, task.Id, rep, m.ttftMs, m.latencyMs, m.decodeTps)
		} else {
			detail := ""
			if m.sample.Error != nil {
				detail = ": " + *m.sample.Error
			}
			opts.progressf("[%d/%d]%s %s rep %d  %s%s", done, total, label, task.Id, rep, m.sample.Status, detail)
		}
		if !warmup && m.sample.Status == schema.SamplesV1JsonSamplesElemStatusOk {
			if m.ttftMs > 0 {
				ttfts = append(ttfts, m.ttftMs)
			}
			if m.latencyMs > 0 {
				latencies = append(latencies, m.latencyMs)
			}
			if m.decodeTps > 0 {
				decodes = append(decodes, m.decodeTps)
			}
			if m.interTokenMs > 0 {
				interTokens = append(interTokens, m.interTokenMs)
			}
		}
		return nil
	}

	for i := 0; i < warmups; i++ {
		task := manifest.Tasks[i%len(manifest.Tasks)]
		if err := execute(task, 0, true); err != nil {
			return nil, err
		}
	}
	for rep := 0; rep < reps; rep++ {
		for _, task := range manifest.Tasks {
			if err := execute(task, rep, false); err != nil {
				return nil, err
			}
		}
	}

	okCount := 0
	for _, s := range samplesOut {
		if (s.Warmup == nil || !*s.Warmup) && s.Status == schema.SamplesV1JsonSamplesElemStatusOk {
			okCount++
		}
	}
	if okCount == 0 {
		return nil, fmt.Errorf("run: every measured request failed — check the target and try `aimark doctor`")
	}

	metrics := aggregate(ttfts, latencies, decodes, interTokens)
	subScores, composite := provisionalScores(metrics, manifest.Scoring)

	runID := results.NewRunID()
	envelope := buildEnvelope(runID, opts, info, metrics, subScores, composite)
	if err := integrity.Sign(&envelope); err != nil {
		return nil, fmt.Errorf("run: sign envelope: %w", err)
	}

	return &Outcome{
		Envelope: envelope,
		Samples: schema.SamplesV1Json{
			SchemaVersion: "aimark.samples.v1",
			RunId:         runID,
			Samples:       samplesOut,
		},
		SubScores: subScores,
		Composite: composite,
	}, nil
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
	}
	if result.InputTokens > 0 {
		in := result.InputTokens
		m.sample.InputTokens = &in
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
	info target.TargetInfo,
	metrics map[string]float64,
	subScores map[string]float64,
	composite float64,
) schema.RunV1Json {
	manifest := opts.Suite.Manifest
	protocolHash := opts.Suite.ProtocolHash()

	params := schema.RunV1JsonTargetParams{
		"temperature": manifest.Protocol.Decoding.Temperature,
		"max_tokens":  manifest.Protocol.Decoding.MaxTokens,
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
