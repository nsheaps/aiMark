package sweep

import (
	"context"
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
)

// CellRun is the outcome of one sweep cell.
type CellRun struct {
	// Params is the full merged parameter vector for the cell.
	Params map[string]any
	// Outcome is the completed run (nil when Err is set).
	Outcome *run.Outcome
	// Err is the cell's failure, if any. Other cells still execute.
	Err error
}

// ExecOptions configures a sweep execution.
type ExecOptions struct {
	// TargetSpec is the base target string (--target); a cell's "model"
	// value replaces its model component.
	TargetSpec string
	// TargetOpts is passed to the target factory for every cell.
	TargetOpts target.Options
	// Base is the run options template (suite, source, reps, ...); per-cell
	// fields (SweepID, Params, overrides) are filled in per cell.
	Base run.Options
	// Save persists each completed cell run (may be nil).
	Save func(*run.Outcome) error
}

// Execute runs every cell of the sweep sequentially, sharing one sweep_id.
// Cell failures are recorded and do not abort the sweep; only context
// cancellation does.
func Execute(ctx context.Context, cfg Config, opts ExecOptions) (sweepID string, cells []CellRun, err error) {
	sweepID = NewSweepID()
	for _, cell := range cfg.Cells() {
		if err := ctx.Err(); err != nil {
			return sweepID, cells, err
		}
		outcome, cellErr := executeCell(ctx, sweepID, cell, opts)
		cells = append(cells, CellRun{Params: cell, Outcome: outcome, Err: cellErr})
	}
	return sweepID, cells, nil
}

// executeCell builds the cell's target and run options and executes one run.
func executeCell(ctx context.Context, sweepID string, cell map[string]any, opts ExecOptions) (*run.Outcome, error) {
	spec := opts.TargetSpec
	if model, ok := String(cell["model"]); ok && model != "" {
		adapter, _, err := target.Parse(spec)
		if err != nil {
			return nil, err
		}
		spec = adapter + ":" + model
	}
	tgt, err := target.New(spec, opts.TargetOpts)
	if err != nil {
		return nil, fmt.Errorf("sweep: cell target %s: %w", spec, err)
	}

	runOpts := opts.Base
	runOpts.Target = tgt
	runOpts.SweepID = sweepID

	// Every cell key is recorded in the run params (merged over the base).
	params := map[string]any{}
	for k, v := range opts.Base.Params {
		params[k] = v
	}
	for k, v := range cell {
		params[k] = v
	}
	runOpts.Params = params

	// Well-known keys also steer the protocol.
	if v, present := cell["temperature"]; present {
		f, ok := Float(v)
		if !ok {
			return nil, fmt.Errorf("sweep: temperature %v is not a number", v)
		}
		runOpts.Temperature = &f
	}
	if v, present := cell["max_tokens"]; present {
		n, ok := Int(v)
		if !ok {
			return nil, fmt.Errorf("sweep: max_tokens %v is not an integer", v)
		}
		runOpts.MaxTokens = &n
	}
	if v, present := cell["concurrency"]; present {
		n, ok := Int(v)
		if !ok || n < 1 {
			return nil, fmt.Errorf("sweep: concurrency %v is not a positive integer", v)
		}
		runOpts.Concurrency = []int{n}
	}

	outcome, err := run.Execute(ctx, runOpts)
	if err != nil {
		return nil, err
	}
	if opts.Save != nil {
		if err := opts.Save(outcome); err != nil {
			return outcome, fmt.Errorf("sweep: save cell run: %w", err)
		}
	}
	return outcome, nil
}
