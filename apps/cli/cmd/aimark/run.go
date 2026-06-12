package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/nsheaps/aimark/apps/cli/internal/target"
	"github.com/spf13/cobra"
)

type runFlags struct {
	target    string
	targetURL string
	apiKey    string
	reps      int
	warmups   int
	asJSON    bool
	quiet     bool
	noColor   bool
	source    string
	params    []string
	estimate  bool
	priceIn   float64
	priceOut  float64
}

func newRunCmd() *cobra.Command {
	flags := runFlags{}
	cmd := &cobra.Command{
		Use:   "run <suite>",
		Short: "Run a benchmark suite against a target",
		Long: `Run a benchmark suite against a target and save the result locally.

Target syntax:
  ollama:<model>   native Ollama (default http://localhost:11434, override with --target-url)
  openai:<model>   any OpenAI-compatible endpoint; --target-url required
                   (vLLM, LM Studio, llama.cpp server, OpenRouter, OpenAI, ...)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBenchmark(cmd, args[0], flags)
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.target, "target", "", "target to benchmark, e.g. ollama:llama3.1:8b (required)")
	f.StringVar(&flags.targetURL, "target-url", "", "override the target base URL")
	f.StringVar(&flags.apiKey, "api-key", "", "API key for openai-compatible targets (or $AIMARK_TARGET_API_KEY / $OPENAI_API_KEY); never stored in results")
	f.IntVar(&flags.reps, "reps", 0, "override the suite's repetitions")
	f.IntVar(&flags.warmups, "warmups", -1, "override the suite's warmup requests")
	f.BoolVar(&flags.asJSON, "json", false, "print the run envelope as JSON to stdout")
	f.BoolVar(&flags.quiet, "quiet", false, "suppress per-request progress output")
	f.BoolVar(&flags.noColor, "no-color", false, "disable colored output")
	f.StringVar(&flags.source, "source", "", "override run source (user|ci|dev); auto-detected by default")
	f.StringArrayVar(&flags.params, "param", nil, "record a parameter k=v in the run envelope (repeatable)")
	f.BoolVar(&flags.estimate, "estimate", false, "print projected token usage and cost, then exit without running")
	f.Float64Var(&flags.priceIn, "price-in", 0, "input price in $ per Mtok (hosted targets, for --estimate and the pricing snapshot)")
	f.Float64Var(&flags.priceOut, "price-out", 0, "output price in $ per Mtok")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

// parseParams converts --param k=v pairs into typed values (bool, number,
// string).
func parseParams(pairs []string) (map[string]any, error) {
	params := map[string]any{}
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --param %q (expected k=v)", pair)
		}
		switch v {
		case "true":
			params[k] = true
		case "false":
			params[k] = false
		default:
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				params[k] = n
			} else {
				params[k] = v
			}
		}
	}
	return params, nil
}

func resolveAPIKey(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("AIMARK_TARGET_API_KEY"); v != "" {
		return v
	}
	return os.Getenv("OPENAI_API_KEY")
}

func runBenchmark(cmd *cobra.Command, suiteArg string, flags runFlags) error {
	suite, err := suites.Get(suiteArg)
	if err != nil {
		return err
	}

	tgt, err := target.New(flags.target, target.Options{
		BaseURL: flags.targetURL,
		APIKey:  resolveAPIKey(flags.apiKey),
	})
	if err != nil {
		return err
	}

	params, err := parseParams(flags.params)
	if err != nil {
		return err
	}

	source, err := run.DetectSource(flags.source)
	if err != nil {
		return err
	}

	if flags.estimate {
		return printEstimate(cmd.OutOrStdout(), suite, flags)
	}

	stderr := cmd.ErrOrStderr()
	var progress func(format string, args ...any)
	if !flags.quiet {
		progress = func(format string, args ...any) {
			fmt.Fprintf(stderr, format+"\n", args...)
		}
		fmt.Fprintf(stderr, "aimark %s — target %s\n", suite.Key, flags.target)
	}

	outcome, err := run.Execute(cmd.Context(), run.Options{
		Suite:    suite,
		Target:   tgt,
		Reps:     flags.reps,
		Warmups:  flags.warmups,
		Source:   source,
		Params:   params,
		PriceIn:  flags.priceIn,
		PriceOut: flags.priceOut,
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
	if !flags.quiet {
		fmt.Fprintf(stderr, "saved %s\n", store.Dir+"/"+outcome.Envelope.RunId+".aimark.json")
	}

	if flags.asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(outcome.Envelope)
	}

	printScoreCard(cmd.OutOrStdout(), suite, outcome)
	fmt.Fprintf(cmd.OutOrStdout(), "\nSubmit with: aimark submit %s\n", outcome.Envelope.RunId)
	return nil
}

// printEstimate projects request counts, token usage, and (when pricing is
// known) cost for the run.
func printEstimate(out io.Writer, suite suites.Suite, flags runFlags) error {
	m := suite.Manifest
	reps := m.Protocol.Repetitions
	if flags.reps > 0 {
		reps = flags.reps
	}
	warmups := m.Protocol.WarmupRequests
	if flags.warmups >= 0 {
		warmups = flags.warmups
	}
	requests := warmups + reps*len(m.Tasks)

	// Rough prompt token estimate: ~4 chars per token.
	promptChars := 0
	for _, t := range m.Tasks {
		promptChars += len(t.Prompt)
		if t.System != nil {
			promptChars += len(*t.System)
		}
	}
	avgInputTokens := 0
	if len(m.Tasks) > 0 {
		avgInputTokens = promptChars / len(m.Tasks) / 4
	}
	inputTokens := requests * avgInputTokens
	outputTokens := requests * m.Protocol.Decoding.MaxTokens // upper bound

	fmt.Fprintf(out, "Estimate for %s against %s\n", suite.Key, flags.target)
	fmt.Fprintf(out, "  requests       %d (%d warmups + %d reps x %d tasks)\n", requests, warmups, reps, len(m.Tasks))
	fmt.Fprintf(out, "  input tokens   ~%d\n", inputTokens)
	fmt.Fprintf(out, "  output tokens  <=%d (max_tokens %d per request)\n", outputTokens, m.Protocol.Decoding.MaxTokens)

	if flags.priceIn > 0 || flags.priceOut > 0 {
		cost := float64(inputTokens)/1e6*flags.priceIn + float64(outputTokens)/1e6*flags.priceOut
		fmt.Fprintf(out, "  projected cost <=$%.4f ($%.2f/Mtok in, $%.2f/Mtok out)\n", cost, flags.priceIn, flags.priceOut)
	} else {
		fmt.Fprintln(out, "  projected cost pricing unknown — pass --price-in/--price-out ($ per Mtok)")
	}
	return nil
}

// printScoreCard renders the final plain-text score summary.
func printScoreCard(out io.Writer, suite suites.Suite, outcome *run.Outcome) {
	env := outcome.Envelope
	line := strings.Repeat("=", 56)
	fmt.Fprintln(out, line)
	fmt.Fprintf(out, "  aiMark %s — %s\n", suite.Key, suite.Manifest.Title)
	fmt.Fprintf(out, "  target: %s", env.Target.Model)
	if env.Target.Runtime != nil {
		fmt.Fprintf(out, " on %s", *env.Target.Runtime)
		if env.Target.RuntimeVersion != nil {
			fmt.Fprintf(out, " %s", *env.Target.RuntimeVersion)
		}
	} else if env.Target.Provider != nil {
		fmt.Fprintf(out, " via %s", *env.Target.Provider)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, line)

	if outcome.Composite > 0 {
		fmt.Fprintf(out, "\n  COMPOSITE SCORE   %8.0f\n\n", outcome.Composite)
	} else {
		fmt.Fprint(out, "\n  COMPOSITE SCORE   (not computable — metrics missing)\n\n")
	}

	names := make([]string, 0, len(outcome.SubScores))
	for name := range outcome.SubScores {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "  %-18s %8.0f\n", name, outcome.SubScores[name])
	}

	fmt.Fprintln(out, "\n  key metrics")
	for _, key := range []string{"ttft_ms_p50", "ttft_ms_p95", "latency_ms_p50", "latency_ms_p95", "decode_tps_mean", "inter_token_ms_mean"} {
		if v, ok := env.Metrics[key]; ok {
			fmt.Fprintf(out, "    %-22s %10.1f\n", key, v)
		}
	}
	fmt.Fprintf(out, "\n  run id: %s (source: %s, provisional — server recompute is canonical)\n", env.RunId, env.Source)
	fmt.Fprintln(out, line)
}
