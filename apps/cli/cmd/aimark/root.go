package main

import (
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/version"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "aimark",
		Short: "The AI system benchmark — one command, one score",
		Long: `aimark is the AI system benchmark: run it with no arguments and it detects
your hardware, assigns a capability class, downloads the pinned runtime and
model assets, runs the fixed test program, and prints one aiMark System
Score you can upload to the public class leaderboards.

Advanced mode (off the class leaderboards): ` + "`aimark run <suite>`" + ` benchmarks
any model on any runtime or hosted API with your own parameters.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		newVersionCmd(),
		newDetectCmd(),
		newDoctorCmd(),
		newSuitesCmd(),
		newBenchCmd(),
		newRunCmd(),
		newMonitorCmd(),
		newResultsCmd(),
		newSubmitCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the aimark version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "aimark %s (%s)\n", version.Version, version.Commit)
			return err
		},
	}
}
