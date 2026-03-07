// Package version provides build metadata injected at compile time via ldflags.
//
// Build with:
//
//	go build -ldflags "-X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.Version=1.0.0 \
//	  -X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.GitCommit=$(git rev-parse HEAD) \
//	  -X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package version

import (
	"fmt"
	"runtime"
)

// Variables set via -ldflags at build time.
var (
	Version   = "dev"     //nolint:gochecknoglobals // injected via ldflags
	GitCommit = "none"    //nolint:gochecknoglobals // injected via ldflags
	BuildTime = "unknown" //nolint:gochecknoglobals // injected via ldflags
)

const shortCommitLen = 7

// Info holds the full set of build metadata.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns the complete build metadata.
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// Short returns a compact version string suitable for display in headers.
// Examples: "dev", "0.1.0-beta.1 (abc1234)".
func Short() string {
	c := shortCommit(GitCommit)
	if c == "" || c == "none" {
		return Version
	}
	return fmt.Sprintf("%s (%s)", Version, c)
}

func shortCommit(s string) string {
	if len(s) > shortCommitLen {
		return s[:shortCommitLen]
	}
	return s
}
