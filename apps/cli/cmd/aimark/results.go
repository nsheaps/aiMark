package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/spf13/cobra"
)

func newResultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "results",
		Short: "Inspect locally saved benchmark results",
	}
	cmd.AddCommand(newResultsListCmd(), newResultsShowCmd(), newResultsExportCmd())
	return cmd
}

func newResultsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := results.Open()
			if err != nil {
				return err
			}
			ids, err := store.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(ids) == 0 {
				fmt.Fprintf(out, "no results in %s — run `aimark run <suite> --target <t>` first\n", store.Dir)
				return nil
			}
			fmt.Fprintf(out, "%-26s %-12s %-28s %9s %-9s %s\n", "RUN", "SUITE", "TARGET", "COMPOSITE", "SUBMITTED", "CREATED")
			for _, id := range ids {
				env, err := store.LoadEnvelope(id)
				if err != nil {
					fmt.Fprintf(out, "%-26s (unreadable: %v)\n", id, err)
					continue
				}
				composite := "-"
				if env.ProvisionalScores != nil && env.ProvisionalScores.Composite != nil {
					composite = fmt.Sprintf("%.0f", *env.ProvisionalScores.Composite)
				}
				submitted := "no"
				if store.IsSubmitted(id) {
					submitted = "yes"
				}
				fmt.Fprintf(out, "%-26s %-12s %-28s %9s %-9s %s\n",
					id,
					fmt.Sprintf("%s-%d", env.Suite.Id, env.Suite.Version),
					env.Target.Model,
					composite,
					submitted,
					env.CreatedAt.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}

func newResultsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one run in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := results.Open()
			if err != nil {
				return err
			}
			env, err := store.LoadEnvelope(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "run     %s (source: %s, created %s)\n", env.RunId, env.Source, env.CreatedAt.Format("2006-01-02 15:04:05 MST"))
			fmt.Fprintf(out, "suite   %s-%d\n", env.Suite.Id, env.Suite.Version)
			fmt.Fprintf(out, "target  %s (%s)\n", env.Target.Model, env.Target.Kind)
			if receipt, err := store.Receipt(env.RunId); err == nil && receipt != nil {
				fmt.Fprintf(out, "submitted  yes")
				if receipt.PublicURL != "" {
					fmt.Fprintf(out, " — %s", receipt.PublicURL)
				}
				fmt.Fprintln(out)
			}

			if env.ProvisionalScores != nil {
				fmt.Fprintln(out, "\nprovisional scores:")
				printScorePtr := func(name string, v *float64) {
					if v != nil {
						fmt.Fprintf(out, "  %-16s %8.0f\n", name, *v)
					}
				}
				printScorePtr("composite", env.ProvisionalScores.Composite)
				printScorePtr("performance", env.ProvisionalScores.Performance)
				printScorePtr("quality", env.ProvisionalScores.Quality)
				printScorePtr("cost_efficiency", env.ProvisionalScores.CostEfficiency)
				printScorePtr("consistency", env.ProvisionalScores.Consistency)
			}

			fmt.Fprintln(out, "\nmetrics:")
			keys := make([]string, 0, len(env.Metrics))
			for k := range env.Metrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(out, "  %-22s %12.2f\n", k, env.Metrics[k])
			}

			if samples, err := store.LoadSamples(env.RunId); err == nil {
				ok, failed := 0, 0
				for _, s := range samples.Samples {
					if s.Status == "ok" {
						ok++
					} else {
						failed++
					}
				}
				fmt.Fprintf(out, "\nsamples: %d total, %d ok, %d failed\n", len(samples.Samples), ok, failed)
			}
			return nil
		},
	}
}

func newResultsExportCmd() *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Export one run envelope as JSON (stdout or --out)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := results.Open()
			if err != nil {
				return err
			}
			env, err := store.LoadEnvelope(args[0])
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(env, "", "  ")
			if err != nil {
				return err
			}
			raw = append(raw, '\n')
			if outPath == "" {
				_, err = cmd.OutOrStdout().Write(raw)
				return err
			}
			if err := os.WriteFile(outPath, raw, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "exported %s to %s\n", env.RunId, outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "", "write to this file instead of stdout")
	return cmd
}
