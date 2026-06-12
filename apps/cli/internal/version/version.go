// Package version holds build-time version metadata, injected by goreleaser
// via -ldflags (see apps/cli/.goreleaser.yaml).
package version

var (
	Version = "dev"
	Commit  = "none"
)
