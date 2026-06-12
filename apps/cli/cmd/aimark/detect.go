package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nsheaps/aimark/apps/cli/internal/hw"
	"github.com/spf13/cobra"
)

// runtimeProbe is one well-known local inference endpoint.
type runtimeProbe struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// VersionPath returns a version string when probing succeeds (optional).
	versionPath string
}

var knownRuntimes = []runtimeProbe{
	{Name: "ollama", URL: "http://localhost:11434", versionPath: "/api/version"},
	{Name: "lmstudio", URL: "http://localhost:1234", versionPath: "/v1/models"},
	{Name: "vllm", URL: "http://localhost:8000", versionPath: "/v1/models"},
}

// runtimeStatus is the probe outcome for one runtime.
type runtimeStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
}

// probeRuntimes best-effort checks the well-known local runtime ports.
func probeRuntimes(ctx context.Context) []runtimeStatus {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	statuses := make([]runtimeStatus, 0, len(knownRuntimes))
	for _, rt := range knownRuntimes {
		status := runtimeStatus{Name: rt.Name, URL: rt.URL}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rt.URL+rt.versionPath, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				status.Reachable = resp.StatusCode < 500
				if rt.Name == "ollama" && resp.StatusCode == http.StatusOK {
					var v struct {
						Version string `json:"version"`
					}
					if json.NewDecoder(resp.Body).Decode(&v) == nil {
						status.Version = v.Version
					}
				}
				_ = resp.Body.Close()
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func newDetectCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detect hardware and probe for running local runtimes",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			profile := hw.Detect(ctx)
			runtimes := probeRuntimes(ctx)

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"hardware": profile,
					"runtimes": runtimes,
				})
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Hardware")
			fmt.Fprintf(out, "  os/arch        %s/%s%s\n", profile.Os, profile.Arch, strPtrSuffix(profile.OsVersion, " (%s)"))
			fmt.Fprintf(out, "  cpu            %s\n", strPtrOr(profile.CpuModel, "unknown"))
			fmt.Fprintf(out, "  cores          %s physical / %s logical\n",
				intPtrOr(profile.CpuCoresPhysical, "?"), intPtrOr(profile.CpuCoresLogical, "?"))
			if profile.RamGb != nil {
				fmt.Fprintf(out, "  ram            %.1f GB\n", *profile.RamGb)
			} else {
				fmt.Fprintln(out, "  ram            unknown")
			}
			if profile.UnifiedMemory != nil && *profile.UnifiedMemory {
				fmt.Fprintln(out, "  unified memory yes")
			}
			if len(profile.Gpus) == 0 {
				fmt.Fprintln(out, "  gpus           none detected")
			}
			for _, g := range profile.Gpus {
				vram := ""
				if g.VramGb != nil {
					vram = fmt.Sprintf(" (%.1f GB)", *g.VramGb)
				}
				fmt.Fprintf(out, "  gpu            %s%s\n", g.Name, vram)
			}
			fmt.Fprintf(out, "  profile id     %s\n", strPtrOr(profile.Id, "unknown"))

			fmt.Fprintln(out, "\nRuntimes")
			for _, rt := range runtimes {
				state := "not detected"
				if rt.Reachable {
					state = "reachable"
					if rt.Version != "" {
						state += " (v" + rt.Version + ")"
					}
				}
				fmt.Fprintf(out, "  %-9s %-26s %s\n", rt.Name, rt.URL, state)
			}
			fmt.Fprintln(out, "\nOpenAI-compatible endpoints elsewhere can't be probed — pass them explicitly:\n  aimark run sprint-1 --target openai:<model> --target-url <url>")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func strPtrOr(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}

func strPtrSuffix(p *string, format string) string {
	if p != nil && *p != "" {
		return fmt.Sprintf(format, *p)
	}
	return ""
}

func intPtrOr(p *int, fallback string) string {
	if p != nil {
		return fmt.Sprintf("%d", *p)
	}
	return fallback
}
