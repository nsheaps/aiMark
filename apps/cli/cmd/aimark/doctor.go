package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/results"
	"github.com/nsheaps/aimark/apps/cli/internal/submit"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var api string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check that aimark can run and submit benchmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ctx := cmd.Context()
			failed := false

			report := func(ok bool, critical bool, name, detail string) {
				mark := "ok  "
				if !ok {
					mark = "FAIL"
					if !critical {
						mark = "warn"
					} else {
						failed = true
					}
				}
				fmt.Fprintf(out, "[%s] %-22s %s\n", mark, name, detail)
			}

			// 1. Results dir writable.
			dir, err := results.DefaultDir()
			if err == nil {
				err = os.MkdirAll(dir, 0o755)
			}
			if err == nil {
				probe := filepath.Join(dir, ".doctor-probe")
				err = os.WriteFile(probe, []byte("ok"), 0o644)
				if err == nil {
					_ = os.Remove(probe)
				}
			}
			if err != nil {
				report(false, true, "results dir", fmt.Sprintf("%v", err))
			} else {
				report(true, true, "results dir", dir+" is writable")
			}

			// 2. Ollama reachable (informational — only needed for local runs).
			report2 := func(url string) {
				client := &http.Client{Timeout: 1500 * time.Millisecond}
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/version", nil)
				resp, err := client.Do(req)
				if err != nil {
					report(false, false, "ollama", url+" not reachable (only needed for ollama targets)")
					return
				}
				defer resp.Body.Close() //nolint:errcheck // best-effort close
				report(resp.StatusCode == http.StatusOK, false, "ollama", url+" reachable")
			}
			report2("http://localhost:11434")

			// 3. API reachable.
			apiBase := submit.ResolveAPI(api)
			{
				client := &http.Client{Timeout: 3 * time.Second}
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/v1/health", nil)
				resp, err := client.Do(req)
				if err != nil {
					report(false, false, "api", apiBase+" not reachable (offline-first: you can run now, submit later)")
				} else {
					_ = resp.Body.Close()
					report(resp.StatusCode == http.StatusOK, false, "api",
						fmt.Sprintf("%s/v1/health -> HTTP %d", apiBase, resp.StatusCode))
				}
			}

			// 4. Clock sanity: monotonic clock advances and wall clock is plausible.
			{
				start := time.Now()
				time.Sleep(10 * time.Millisecond)
				elapsed := time.Since(start) // uses the monotonic reading
				monotonicOK := elapsed > 0
				wallOK := time.Now().Year() >= 2024
				switch {
				case !monotonicOK:
					report(false, true, "clock", "monotonic clock did not advance")
				case !wallOK:
					report(false, true, "clock", fmt.Sprintf("wall clock looks wrong: %s", time.Now().Format(time.RFC3339)))
				default:
					report(true, true, "clock", "monotonic + wall clock sane")
				}
			}

			if failed {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&api, "api", "", "API base URL (default $AIMARK_API or "+submit.DefaultAPI+")")
	return cmd
}
