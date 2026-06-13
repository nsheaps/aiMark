package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/assets"
	"github.com/nsheaps/aimark/apps/cli/internal/bench"
	"github.com/nsheaps/aimark/apps/cli/internal/hw"
	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/run"
	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/submit"
	"github.com/spf13/cobra"
)

type benchFlags struct {
	yes      bool
	noUpload bool
	asJSON   bool
	quiet    bool
	api      string
	source   string
	class    string
}

func newBenchCmd() *cobra.Command {
	flags := benchFlags{}
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run the zero-choice aiMark system benchmark (the default command)",
		Long: `Run the zero-choice aiMark system benchmark.

aimark detects your hardware, assigns a capability class, downloads the
pinned runtime + model assets for that class, runs the fixed test program,
and prints one aiMark System Score. Bare ` + "`aimark`" + ` runs this command.

You choose nothing: the program (engine build, models, suites, weights) is
frozen per benchmark version, like a 3DMark release. Scores are comparable
within (program version, class).

Dev/e2e-only environment variables (not for scoring real systems):
  AIMARK_BENCH_RUNTIME_URL  point cells at an existing OpenAI-compatible
                            server, skipping asset download and the managed
                            llama-server (classification still runs)
  AIMARK_BENCH_FAST         trim the program for smoke tests (reps capped
                            at 1, marathon concurrency reduced to [1 2])`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBench(cmd, flags)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&flags.yes, "yes", false, "skip prompts: download without asking and upload the result")
	f.BoolVar(&flags.noUpload, "no-upload", false, "never upload; keep the result local (submit later with `aimark submit --all-pending`)")
	f.BoolVar(&flags.asJSON, "json", false, "print the benchmark envelope as JSON to stdout")
	f.BoolVar(&flags.quiet, "quiet", false, "suppress progress output")
	f.StringVar(&flags.api, "api", "", "API base URL (default $AIMARK_API or "+submit.DefaultAPI+")")
	f.StringVar(&flags.source, "source", "", "override benchmark source (user|ci|dev); auto-detected by default")
	f.StringVar(&flags.class, "class", "", "force a capability class (debug; marks the benchmark source dev so it never enters user leaderboards)")
	return cmd
}

// stdinIsTTY reports whether stdin is an interactive terminal.
func stdinIsTTY(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// confirm asks a yes/no question on the command's input.
func confirm(cmd *cobra.Command, out io.Writer, question string, defaultYes bool) bool {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(out, "%s %s ", question, suffix)
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

func runBench(cmd *cobra.Command, flags benchFlags) error {
	stderr := cmd.ErrOrStderr()
	stdout := cmd.OutOrStdout()

	program, err := bench.Get(bench.DefaultProgram)
	if err != nil {
		return err
	}

	source, err := run.DetectSource(flags.source)
	if err != nil {
		return err
	}
	if flags.class != "" && source == schema.RunV1JsonSourceUser {
		source = schema.RunV1JsonSourceDev
		if !flags.quiet {
			fmt.Fprintln(stderr, "--class overrides classification: source forced to dev (kept off user leaderboards)")
		}
	}

	var progress func(format string, args ...any)
	if !flags.quiet {
		progress = func(format string, args ...any) {
			fmt.Fprintf(stderr, format+"\n", args...)
		}
	}

	// Banner + 2-line detect summary.
	profile := hw.Detect(cmd.Context())
	if !flags.quiet {
		fmt.Fprintf(stderr, "aiMark %s — %s\n", program.Key, program.Manifest.Title)
		fmt.Fprintln(stderr, detectSummary(profile))
	}

	isTTY := stdinIsTTY(cmd)
	confirmDownload := func(plan bench.DownloadPlan) bool {
		if flags.yes || plan.TotalBytes == 0 {
			return true
		}
		if !isTTY {
			fmt.Fprintln(stderr, "non-interactive session: proceeding with download (abort with Ctrl-C)")
			return true
		}
		return confirm(cmd, stderr, fmt.Sprintf("Download %s of benchmark assets?", assets.HumanBytes(plan.TotalBytes)), true)
	}

	outcome, err := bench.Execute(cmd.Context(), bench.Options{
		Program:         program,
		Source:          source,
		ClassOverride:   flags.class,
		Progress:        progress,
		ConfirmDownload: confirmDownload,
	})
	if err != nil {
		return err
	}

	// Persist everything before talking to the network.
	store, err := results.Open()
	if err != nil {
		return err
	}
	for _, cell := range outcome.Cells {
		if err := store.Save(cell.Outcome.Envelope, cell.Outcome.Samples); err != nil {
			return err
		}
	}
	if err := store.SaveBench(outcome.Envelope); err != nil {
		return err
	}
	if !flags.quiet {
		fmt.Fprintf(stderr, "saved benchmark %s and %d cell runs to %s\n", outcome.Envelope.BenchId, len(outcome.Cells), store.Dir)
	}

	if outcome.ValidityFailed {
		fmt.Fprintln(stderr, "WARNING: a validity cell failed its accuracy floor — this benchmark will be flagged by the server")
	}

	if flags.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(outcome.Envelope); err != nil {
			return err
		}
	} else {
		printBenchScoreCard(stdout, program, outcome)
	}

	// Upload decision.
	switch {
	case flags.noUpload:
		fmt.Fprintln(stderr, "upload skipped (--no-upload) — submit later with `aimark submit --all-pending`")
		return nil
	case flags.yes:
		// proceed
	case !isTTY:
		fmt.Fprintln(stderr, "not uploading (non-interactive) — re-run with --yes to upload, or `aimark submit --all-pending` later")
		return nil
	default:
		if !confirm(cmd, stderr, "Upload this benchmark to the aiMark leaderboards?", true) {
			fmt.Fprintln(stderr, "upload skipped — submit later with `aimark submit --all-pending`")
			return nil
		}
	}

	return uploadBenchmark(cmd.Context(), stderr, store, submit.ResolveAPI(flags.api), outcome.Envelope)
}

// detectSummary renders the 2-line hardware summary.
func detectSummary(p schema.HardwareProfile) string {
	cpu := "unknown CPU"
	if p.CpuModel != nil {
		cpu = *p.CpuModel
	}
	cores := ""
	if p.CpuCoresLogical != nil {
		cores = fmt.Sprintf(", %d threads", *p.CpuCoresLogical)
	}
	ram := ""
	if p.RamGb != nil {
		ram = fmt.Sprintf(", %.0f GB RAM", *p.RamGb)
	}
	line1 := fmt.Sprintf("  %s%s%s (%s/%s)", cpu, cores, ram, p.Os, p.Arch)

	gpu := "no GPU detected"
	if len(p.Gpus) > 0 {
		parts := make([]string, 0, len(p.Gpus))
		for _, g := range p.Gpus {
			s := g.Name
			if g.VramGb != nil {
				s += fmt.Sprintf(" (%.0f GB)", *g.VramGb)
			}
			parts = append(parts, s)
		}
		gpu = strings.Join(parts, ", ")
	}
	if p.UnifiedMemory != nil && *p.UnifiedMemory {
		gpu += " — unified memory"
	}
	return line1 + "\n  " + gpu
}

// printBenchScoreCard renders the final system score card.
func printBenchScoreCard(out io.Writer, program bench.Program, outcome *bench.Outcome) {
	classTitle := outcome.Envelope.Class
	for _, cls := range program.Manifest.Classes {
		if cls.Id == outcome.Envelope.Class {
			classTitle = cls.Title
		}
	}

	line := strings.Repeat("=", 64)
	fmt.Fprintln(out, line)
	if outcome.Composite > 0 {
		fmt.Fprintf(out, "  aiMark System Score: %.0f · class %s · %s\n", outcome.Composite, classTitle, program.Key)
	} else {
		fmt.Fprintf(out, "  aiMark System Score: (not computable) · class %s · %s\n", classTitle, program.Key)
	}
	fmt.Fprintln(out, line)
	if outcome.Performance > 0 {
		fmt.Fprintf(out, "  %-18s %8.0f\n", "performance", outcome.Performance)
	}
	if outcome.Consistency > 0 {
		fmt.Fprintf(out, "  %-18s %8.0f\n", "consistency", outcome.Consistency)
	}

	fmt.Fprintf(out, "\n  %-20s %-18s %-9s %7s %10s\n", "cell", "model", "role", "weight", "composite")
	for _, cell := range outcome.Cells {
		composite := "-"
		if cell.Outcome.Composite > 0 {
			composite = fmt.Sprintf("%.0f", cell.Outcome.Composite)
		}
		role := string(cell.Cell.Role)
		if cell.ValidityFailed {
			role = "validity ✗"
		}
		weight := "-"
		if cell.Cell.Weight > 0 {
			weight = fmt.Sprintf("%.0f", cell.Cell.Weight)
		}
		fmt.Fprintf(out, "  %-20s %-18s %-9s %7s %10s\n", cell.Cell.ID, cell.Cell.Model.Id, role, weight, composite)
	}
	fmt.Fprintf(out, "\n  bench id: %s (source: %s, provisional — server recompute is canonical)\n",
		outcome.Envelope.BenchId, outcome.Envelope.Source)
	fmt.Fprintln(out, line)
}

// uploadBenchmark submits the cell runs first, then the benchmark envelope.
// A server without the benchmark endpoint is not an error: cells land, the
// benchmark stays pending locally.
func uploadBenchmark(
	ctx context.Context,
	out io.Writer,
	store *results.Store,
	apiBase string,
	env schema.BenchmarkV1Json,
) error {
	client := submit.New(apiBase)

	failures := 0
	for _, cell := range env.Cells {
		if store.IsSubmitted(cell.RunId) {
			continue
		}
		runEnv, err := store.LoadEnvelope(cell.RunId)
		if err != nil {
			fmt.Fprintf(out, "%s: %v\n", cell.RunId, err)
			failures++
			continue
		}
		resp, err := client.Submit(ctx, runEnv)
		switch {
		case errors.Is(err, submit.ErrDuplicate):
			_ = store.MarkSubmitted(cell.RunId, results.SubmitReceipt{RunID: cell.RunId})
		case err != nil:
			fmt.Fprintf(out, "cell %s (%s): %v\n", cell.CellId, cell.RunId, err)
			failures++
		default:
			_ = store.MarkSubmitted(cell.RunId, results.SubmitReceipt{
				RunID: resp.RunID, ClaimToken: resp.ClaimToken, PublicURL: resp.PublicURL,
			})
			fmt.Fprintf(out, "cell %s submitted (%s)\n", cell.CellId, cell.RunId)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d cell submission(s) failed — benchmark not submitted (retry with `aimark submit --all-pending`)", failures)
	}

	resp, err := client.SubmitBenchmark(ctx, env)
	if errors.Is(err, submit.ErrBenchmarksUnsupported) {
		fmt.Fprintf(out, "NOTE: %s — cell runs were submitted; the benchmark envelope is kept locally\n", err)
		fmt.Fprintln(out, "      and will upload via `aimark submit --all-pending` once the server supports it.")
		return nil
	}
	if errors.Is(err, submit.ErrDuplicate) {
		fmt.Fprintf(out, "benchmark %s already submitted\n", env.BenchId)
		return store.MarkSubmitted(env.BenchId, results.SubmitReceipt{BenchID: env.BenchId})
	}
	if err != nil {
		return err
	}

	benchID := resp.BenchID
	if benchID == "" {
		benchID = env.BenchId
	}
	receipt := results.SubmitReceipt{BenchID: benchID, ClaimToken: resp.ClaimToken, PublicURL: resp.PublicURL}
	if err := store.MarkSubmitted(env.BenchId, receipt); err != nil {
		return err
	}
	fmt.Fprintf(out, "benchmark %s submitted\n", env.BenchId)
	if resp.PublicURL != "" {
		fmt.Fprintf(out, "  public:      %s\n", resp.PublicURL)
	}
	if resp.ClaimToken != "" {
		fmt.Fprintf(out, "  claim token: %s (saved to %s.submitted.json)\n", resp.ClaimToken, env.BenchId)
	}
	return nil
}
