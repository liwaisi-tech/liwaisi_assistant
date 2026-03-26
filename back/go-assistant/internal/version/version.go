// Package version provides build-time version information.
package version

import "fmt"

// These variables are set at build time via -ldflags.
var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

// String returns a formatted version string.
func String() string {
	return fmt.Sprintf("liwaisi %s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
}
