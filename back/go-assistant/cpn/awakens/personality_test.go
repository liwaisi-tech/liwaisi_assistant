package awakens

import (
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func TestEnvironmentAwarenessBlock_Shape(t *testing.T) {
	t.Parallel()
	snap := persist.HostCapabilitySnapshot{
		HostID:   "host-1",
		Identity: persist.HostIdentity{User: "app", Shell: "/bin/sh"},
		Kernel: persist.HostKernel{
			OS:        "Linux",
			Arch:      "aarch64",
			Kernel:    "6.17",
			OSRelease: map[string]string{"NAME": "Alpine Linux", "VERSION_ID": "3.21"},
		},
		Binaries: []persist.BinaryProbe{
			{Name: "sh", Present: true},
			{Name: "awk", Present: true},
			{Name: "git", Present: false},
			{Name: "python3", Present: false},
		},
	}
	block := EnvironmentAwarenessBlock(snap)
	if !strings.HasPrefix(block, EnvironmentAwarenessHeader) {
		t.Fatalf("block must start with header; got %q", block)
	}
	for _, want := range []string{
		"Alpine Linux 3.21", "aarch64", "kernel 6.17", "/bin/sh",
		"awk, sh", "git, python3",
		"alternative",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
}

func TestEnvironmentAwarenessBlock_Empty(t *testing.T) {
	t.Parallel()
	if EnvironmentAwarenessBlock(persist.HostCapabilitySnapshot{}) != "" {
		t.Fatalf("empty snapshot must produce empty block")
	}
}
