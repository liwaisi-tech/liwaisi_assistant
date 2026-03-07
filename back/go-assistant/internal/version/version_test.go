package version

import (
	"runtime"
	"testing"
)

func TestGet_DefaultValues(t *testing.T) {
	info := Get()

	if info.Version != Version {
		t.Errorf("Get().Version = %q, want %q", info.Version, Version)
	}
	if info.GitCommit != GitCommit {
		t.Errorf("Get().GitCommit = %q, want %q", info.GitCommit, GitCommit)
	}
	if info.BuildTime != BuildTime {
		t.Errorf("Get().BuildTime = %q, want %q", info.BuildTime, BuildTime)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("Get().GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS {
		t.Errorf("Get().OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Get().Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}

func TestShort(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		gitCommit string
		want      string
	}{
		{
			name:      "dev build with no ldflags",
			version:   "dev",
			gitCommit: "none",
			want:      "dev",
		},
		{
			name:      "dev build with real commit hash",
			version:   "dev",
			gitCommit: "abc1234567890",
			want:      "dev (abc1234)",
		},
		{
			name:      "release with full commit hash",
			version:   "0.1.0-beta.1",
			gitCommit: "abc1234567890abcdef1234567890abcdef123456",
			want:      "0.1.0-beta.1 (abc1234)",
		},
		{
			name:      "release without commit",
			version:   "0.1.0",
			gitCommit: "none",
			want:      "0.1.0",
		},
		{
			name:      "release with empty commit",
			version:   "0.1.0",
			gitCommit: "",
			want:      "0.1.0",
		},
		{
			name:      "release with short commit",
			version:   "1.2.3",
			gitCommit: "abc",
			want:      "1.2.3 (abc)",
		},
		{
			name:      "release with exactly 7-char commit",
			version:   "2.0.0",
			gitCommit: "abc1234",
			want:      "2.0.0 (abc1234)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVersion := Version
			origCommit := GitCommit
			t.Cleanup(func() {
				Version = origVersion
				GitCommit = origCommit
			})

			Version = tt.version
			GitCommit = tt.gitCommit

			got := Short()
			if got != tt.want {
				t.Errorf("Short() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "long hash", input: "abc1234567890", want: "abc1234"},
		{name: "short string", input: "abc", want: "abc"},
		{name: "empty string", input: "", want: ""},
		{name: "exactly 7 chars", input: "abc1234", want: "abc1234"},
		{name: "8 chars", input: "abc12345", want: "abc1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortCommit(tt.input)
			if got != tt.want {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
