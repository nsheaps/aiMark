package main

import (
	"fmt"
	"os"

	"github.com/nsheaps/aimark/apps/cli/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "aimark",
		Short: "Benchmark AI models across runtimes, providers, and hardware",
		Long: `aimark runs standardized, versioned benchmark suites against AI models —
local runtimes (Ollama, llama.cpp, vLLM, LM Studio) and hosted APIs — and
produces comparable scores you can submit to the public aiMark leaderboards.`,
		SilenceUsage: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the aimark version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("aimark %s (%s)\n", version.Version, version.Commit)
		},
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
