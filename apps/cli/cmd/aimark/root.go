package main

import (
	"fmt"

	"github.com/nsheaps/aimark/apps/cli/internal/version"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "aimark",
		Short: "Benchmark AI models across runtimes, providers, and hardware",
		Long: `aimark runs standardized, versioned benchmark suites against AI models —
local runtimes (Ollama, llama.cpp, vLLM, LM Studio) and hosted APIs — and
produces comparable scores you can submit to the public aiMark leaderboards.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		newVersionCmd(),
		newDetectCmd(),
		newDoctorCmd(),
		newSuitesCmd(),
		newRunCmd(),
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
