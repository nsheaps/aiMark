package main

import (
	"os"
	"strings"
)

func main() {
	root := newRootCmd()
	args := os.Args[1:]
	if defaultsToBench(args) {
		args = append([]string{"bench"}, args...)
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// defaultsToBench reports whether a bare `aimark` invocation (no subcommand)
// should run the zero-choice benchmark: no args at all, or only flags —
// `aimark --yes` means `aimark bench --yes`. Help/version requests keep
// their root-level meaning.
func defaultsToBench(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if !strings.HasPrefix(args[0], "-") {
		return false // subcommand (or a typo cobra will report)
	}
	switch args[0] {
	case "-h", "--help", "-v", "--version":
		return false
	}
	return true
}
