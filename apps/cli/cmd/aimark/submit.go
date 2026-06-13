package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/submit"
	"github.com/spf13/cobra"
)

func newSubmitCmd() *cobra.Command {
	var (
		api        string
		allPending bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "submit [<id>]",
		Short: "Submit saved runs and benchmarks to the aiMark leaderboards",
		Long: `Submit a saved run or benchmark envelope to the aiMark API. Offline-first:
run now, submit whenever you're online. Raw samples stay local for now.

--all-pending submits every unsubmitted run first (benchmark cells are
runs), then every unsubmitted benchmark envelope.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := results.Open()
			if err != nil {
				return err
			}

			var runIDs, benchIDs []string
			switch {
			case allPending && len(args) > 0:
				return fmt.Errorf("pass either <id> or --all-pending, not both")
			case allPending:
				runIDs, err = store.Pending()
				if err != nil {
					return err
				}
				benchIDs, err = store.PendingBench()
				if err != nil {
					return err
				}
				if len(runIDs) == 0 && len(benchIDs) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "nothing to submit — all saved runs and benchmarks are already submitted")
					return nil
				}
			case len(args) == 1:
				// A bare id can name a run or a benchmark; the file decides.
				if _, err := store.LoadBench(args[0]); err == nil {
					benchIDs = []string{args[0]}
				} else {
					runIDs = []string{args[0]}
				}
			default:
				return fmt.Errorf("pass a run/bench id or --all-pending (see `aimark results list`)")
			}

			apiBase := submit.ResolveAPI(api)
			out := cmd.OutOrStdout()

			if !yes {
				total := len(runIDs) + len(benchIDs)
				fmt.Fprintf(out, "Submitting %d item(s) to %s. Continue? [y/N] ", total, apiBase)
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, _ := reader.ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(out, "aborted")
					return nil
				}
			}

			client := submit.New(apiBase)
			failures := 0
			failures += submitRuns(cmd.Context(), out, store, client, runIDs, allPending)
			failures += submitBenches(cmd.Context(), out, store, client, benchIDs)

			if failures > 0 {
				return fmt.Errorf("%d of %d submission(s) failed", failures, len(runIDs)+len(benchIDs))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&api, "api", "", "API base URL (default $AIMARK_API or "+submit.DefaultAPI+")")
	cmd.Flags().BoolVar(&allPending, "all-pending", false, "submit every saved run and benchmark that has not been submitted yet")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

// submitRuns pushes run envelopes and returns the failure count.
func submitRuns(
	ctx context.Context,
	out io.Writer,
	store *results.Store,
	client *submit.Client,
	ids []string,
	skipSubmitted bool,
) int {
	failures := 0
	for _, id := range ids {
		if store.IsSubmitted(id) && skipSubmitted {
			continue
		}
		env, err := store.LoadEnvelope(id)
		if err != nil {
			fmt.Fprintf(out, "%s: %v\n", id, err)
			failures++
			continue
		}
		resp, err := client.Submit(ctx, env)
		switch {
		case errors.Is(err, submit.ErrDuplicate):
			fmt.Fprintf(out, "%s: already on the leaderboard (HTTP 409) — marking as submitted locally\n", id)
			if mErr := store.MarkSubmitted(id, results.SubmitReceipt{RunID: id}); mErr != nil {
				fmt.Fprintf(out, "%s: failed to record submission: %v\n", id, mErr)
			}
			continue
		case err != nil:
			var verr *submit.ValidationError
			if errors.As(err, &verr) {
				fmt.Fprintf(out, "%s: %v\n      fix the run or re-run the benchmark; this result will not be accepted as-is\n", id, verr)
			} else {
				fmt.Fprintf(out, "%s: %v\n", id, err)
			}
			failures++
			continue
		}

		receipt := results.SubmitReceipt{
			RunID:      resp.RunID,
			ClaimToken: resp.ClaimToken,
			PublicURL:  resp.PublicURL,
		}
		if err := store.MarkSubmitted(id, receipt); err != nil {
			fmt.Fprintf(out, "%s: submitted, but failed to persist the receipt: %v\n", id, err)
			failures++
			continue
		}
		fmt.Fprintf(out, "%s: submitted\n", id)
		if resp.PublicURL != "" {
			fmt.Fprintf(out, "      public:      %s\n", resp.PublicURL)
		}
		if resp.ClaimToken != "" {
			fmt.Fprintf(out, "      claim token: %s (saved to %s.submitted.json)\n", resp.ClaimToken, id)
		}
	}
	return failures
}

// submitBenches pushes benchmark envelopes and returns the failure count. A
// server without the benchmark endpoint keeps envelopes pending without
// counting as a failure.
func submitBenches(
	ctx context.Context,
	out io.Writer,
	store *results.Store,
	client *submit.Client,
	ids []string,
) int {
	failures := 0
	for _, id := range ids {
		env, err := store.LoadBench(id)
		if err != nil {
			fmt.Fprintf(out, "%s: %v\n", id, err)
			failures++
			continue
		}
		resp, err := client.SubmitBenchmark(ctx, env)
		switch {
		case errors.Is(err, submit.ErrBenchmarksUnsupported):
			fmt.Fprintf(out, "%s: %v — kept pending locally\n", id, err)
			continue
		case errors.Is(err, submit.ErrDuplicate):
			fmt.Fprintf(out, "%s: benchmark already submitted (HTTP 409) — marking as submitted locally\n", id)
			if mErr := store.MarkSubmitted(id, results.SubmitReceipt{BenchID: id}); mErr != nil {
				fmt.Fprintf(out, "%s: failed to record submission: %v\n", id, mErr)
			}
			continue
		case err != nil:
			fmt.Fprintf(out, "%s: %v\n", id, err)
			failures++
			continue
		}

		benchID := resp.BenchID
		if benchID == "" {
			benchID = id
		}
		receipt := results.SubmitReceipt{
			BenchID:    benchID,
			ClaimToken: resp.ClaimToken,
			PublicURL:  resp.PublicURL,
		}
		if err := store.MarkSubmitted(id, receipt); err != nil {
			fmt.Fprintf(out, "%s: submitted, but failed to persist the receipt: %v\n", id, err)
			failures++
			continue
		}
		fmt.Fprintf(out, "%s: benchmark submitted\n", id)
		if resp.PublicURL != "" {
			fmt.Fprintf(out, "      public:      %s\n", resp.PublicURL)
		}
		if resp.ClaimToken != "" {
			fmt.Fprintf(out, "      claim token: %s (saved to %s.submitted.json)\n", resp.ClaimToken, id)
		}
	}
	return failures
}
