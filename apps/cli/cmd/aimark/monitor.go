package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/submit"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
	"github.com/spf13/cobra"
)

// monitorReps is the fixed repetition count for cloud monitor probes —
// enough for a stable latency snapshot, cheap enough for a cron schedule.
const monitorReps = 3

func newMonitorCmd() *cobra.Command {
	var (
		provider  string
		model     string
		apiKey    string
		api       string
		targetURL string
		asJSON    bool
		quiet     bool
	)
	cmd := &cobra.Command{
		Use:   "monitor --provider <p> --model <m>",
		Short: "Probe a hosted API for the cloud reference series (sprint-1, 3 reps)",
		Long: `Probe a hosted provider and submit the measurement to the aiMark API.

This is the payload generator for the scheduled cloud-monitoring workflow:
it runs sprint-1 only (3 repetitions) against the hosted adapter and submits
the result as a normal run flagged with the {"monitor": true} parameter.
The site shows the series as a reference band, never a leaderboard entry.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, opts, err := monitorTarget(provider, model, apiKey, targetURL)
			if err != nil {
				return err
			}
			tgt, err := target.New(spec, opts)
			if err != nil {
				return err
			}

			suite, err := suites.Get("sprint-1")
			if err != nil {
				return err
			}
			source, err := run.DetectSource("")
			if err != nil {
				return err
			}

			stderr := cmd.ErrOrStderr()
			var progress func(format string, args ...any)
			if !quiet {
				progress = func(format string, args ...any) {
					fmt.Fprintf(stderr, format+"\n", args...)
				}
				fmt.Fprintf(stderr, "aimark monitor — %s:%s (sprint-1, %d reps)\n", provider, model, monitorReps)
			}

			outcome, err := run.Execute(cmd.Context(), run.Options{
				Suite:    suite,
				Target:   tgt,
				Reps:     monitorReps,
				Warmups:  -1,
				Source:   source,
				Params:   map[string]any{"monitor": true},
				Progress: progress,
			})
			if err != nil {
				return err
			}

			store, err := results.Open()
			if err != nil {
				return err
			}
			if err := store.Save(outcome.Envelope, outcome.Samples); err != nil {
				return err
			}

			client := submit.New(submit.ResolveAPI(api))
			resp, err := client.Submit(cmd.Context(), outcome.Envelope)
			switch {
			case errors.Is(err, submit.ErrDuplicate):
				fmt.Fprintf(stderr, "%s: already submitted\n", outcome.Envelope.RunId)
			case err != nil:
				return fmt.Errorf("monitor: probe measured but submit failed: %w", err)
			default:
				if err := store.MarkSubmitted(outcome.Envelope.RunId, results.SubmitReceipt{
					RunID: resp.RunID, ClaimToken: resp.ClaimToken, PublicURL: resp.PublicURL,
				}); err != nil {
					return err
				}
				if !quiet {
					fmt.Fprintf(stderr, "submitted %s\n", outcome.Envelope.RunId)
				}
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(outcome.Envelope)
			}
			if !quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "monitor probe %s: composite %.0f (run %s)\n",
					provider, outcome.Composite, outcome.Envelope.RunId)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&provider, "provider", "", "hosted provider: openai, anthropic, or google (required)")
	f.StringVar(&model, "model", "", "model id to probe (required)")
	f.StringVar(&apiKey, "api-key", "", "provider API key (or the provider's conventional env var); never stored in results")
	f.StringVar(&api, "api", "", "aiMark API base URL (default $AIMARK_API or "+submit.DefaultAPI+")")
	f.StringVar(&targetURL, "target-url", "", "override the provider base URL (testing)")
	f.BoolVar(&asJSON, "json", false, "print the run envelope as JSON to stdout")
	f.BoolVar(&quiet, "quiet", false, "suppress progress output")
	_ = cmd.MarkFlagRequired("provider")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

// monitorTarget maps a provider to a target spec + options.
func monitorTarget(provider, model, apiKey, targetURL string) (string, target.Options, error) {
	if model == "" {
		return "", target.Options{}, fmt.Errorf("monitor: --model is required")
	}
	opts := target.Options{BaseURL: targetURL, APIKey: resolveAPIKey(apiKey, provider)}
	switch provider {
	case "openai":
		if opts.BaseURL == "" {
			opts.BaseURL = "https://api.openai.com"
		}
		return "openai:" + model, opts, nil
	case "anthropic":
		return "anthropic:" + model, opts, nil
	case "google":
		return "google:" + model, opts, nil
	default:
		return "", target.Options{}, fmt.Errorf("monitor: invalid --provider %q (must be openai, anthropic, or google)", provider)
	}
}
