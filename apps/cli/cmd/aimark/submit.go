package main

import (
	"bufio"
	"errors"
	"fmt"
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
		Short: "Submit saved runs to the aiMark leaderboards",
		Long: `Submit a saved run envelope to the aiMark API. Offline-first: run now,
submit whenever you're online. Raw samples stay local for now.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := results.Open()
			if err != nil {
				return err
			}

			var ids []string
			switch {
			case allPending && len(args) > 0:
				return fmt.Errorf("pass either <id> or --all-pending, not both")
			case allPending:
				ids, err = store.Pending()
				if err != nil {
					return err
				}
				if len(ids) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "nothing to submit — all saved runs are already submitted")
					return nil
				}
			case len(args) == 1:
				ids = []string{args[0]}
			default:
				return fmt.Errorf("pass a run id or --all-pending (see `aimark results list`)")
			}

			apiBase := submit.ResolveAPI(api)
			out := cmd.OutOrStdout()

			if !yes {
				fmt.Fprintf(out, "Submitting %d run(s) to %s. Continue? [y/N] ", len(ids), apiBase)
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, _ := reader.ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(out, "aborted")
					return nil
				}
			}

			client := submit.New(apiBase)
			var failures int
			for _, id := range ids {
				if store.IsSubmitted(id) && allPending {
					continue
				}
				env, err := store.LoadEnvelope(id)
				if err != nil {
					fmt.Fprintf(out, "%s: %v\n", id, err)
					failures++
					continue
				}
				resp, err := client.Submit(cmd.Context(), env)
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

			if failures > 0 {
				return fmt.Errorf("%d of %d submission(s) failed", failures, len(ids))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&api, "api", "", "API base URL (default $AIMARK_API or "+submit.DefaultAPI+")")
	cmd.Flags().BoolVar(&allPending, "all-pending", false, "submit every saved run that has not been submitted yet")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}
