package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/nsheaps/aimark/apps/cli/internal/schema"
	"github.com/nsheaps/aimark/apps/cli/internal/suites"
	"github.com/spf13/cobra"
)

func newSuitesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suites",
		Short: "Inspect the embedded benchmark suites",
	}
	cmd.AddCommand(newSuitesListCmd(), newSuitesInfoCmd())
	return cmd
}

func newSuitesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List embedded suites",
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := suites.All()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-12s %-20s %-8s %6s %6s\n", "SUITE", "TITLE", "STATUS", "TASKS", "REPS")
			for _, s := range all {
				fmt.Fprintf(out, "%-12s %-20s %-8s %6d %6d\n",
					s.Key, s.Manifest.Title, s.Manifest.Status,
					len(s.Manifest.Tasks), s.Manifest.Protocol.Repetitions)
			}
			return nil
		},
	}
}

func newSuitesInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <id-version>",
		Short: "Show one suite in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := suites.Get(args[0])
			if err != nil {
				return err
			}
			m := s.Manifest
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s — %s\n", s.Key, m.Title)
			if m.Description != nil {
				fmt.Fprintf(out, "%s\n", *m.Description)
			}
			tracks := make([]string, 0, len(m.Tracks))
			for _, t := range m.Tracks {
				tracks = append(tracks, string(t))
			}
			fmt.Fprintf(out, "\nstatus:    %s\n", m.Status)
			fmt.Fprintf(out, "tracks:    %s\n", strings.Join(tracks, ", "))
			fmt.Fprintf(out, "protocol:  %d warmups, %d reps, timeout %dms, streaming=%t, temp=%g, max_tokens=%d\n",
				m.Protocol.WarmupRequests, m.Protocol.Repetitions, m.Protocol.RequestTimeoutMs,
				m.Protocol.Streaming, m.Protocol.Decoding.Temperature, m.Protocol.Decoding.MaxTokens)
			fmt.Fprintf(out, "hash:      %s\n", s.ProtocolHash())

			fmt.Fprintf(out, "\ntasks (%d):\n", len(m.Tasks))
			for _, t := range m.Tasks {
				prompt := t.Prompt
				if len(prompt) > 70 {
					prompt = prompt[:67] + "..."
				}
				fmt.Fprintf(out, "  %-12s %s\n", t.Id, prompt)
			}

			fmt.Fprintln(out, "\nscoring:")
			printScoring(out, s)
			return nil
		},
	}
}

func printScoring(out io.Writer, s suites.Suite) {
	scoring := s.Manifest.Scoring
	named := []struct {
		name string
		spec *schema.SubScoreSpec
	}{
		{"performance", scoring.Performance},
		{"quality", scoring.Quality},
		{"cost_efficiency", scoring.CostEfficiency},
		{"consistency", scoring.Consistency},
	}
	for _, n := range named {
		if n.spec == nil {
			continue
		}
		fmt.Fprintf(out, "  %s (composite weight %g):\n", n.name, scoring.CompositeWeights[n.name])
		for _, m := range n.spec.Metrics {
			direction := "higher is better"
			if m.LowerIsBetter != nil && *m.LowerIsBetter {
				direction = "lower is better"
			}
			fmt.Fprintf(out, "    %-20s ref=%-8g weight=%-4g %s\n", m.Key, m.Reference, m.Weight, direction)
		}
	}
}
