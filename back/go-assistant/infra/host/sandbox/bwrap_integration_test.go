//go:build sandbox_integration

package sandbox_test

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/host/sandbox"
)

// TestBwrap_RealRuntime exercises the actual bwrap command. Skips when
// bwrap is not present on the CI runner.
func TestBwrap_RealRuntime(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed; skipping real-runtime integration test")
	}

	w := sandbox.New(sandbox.NewDefaultCapability())
	req := cpn.ExecRequest{Command: "echo", Args: []string{"hi"}, Timeout: 5 * time.Second}
	wrapped, err := w.Wrap(req, cpn.SandboxReadonly)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	adapter := host.NewOSHostAdapter(slog.Default())
	res, err := adapter.Exec(context.Background(), wrapped)
	if err != nil {
		t.Fatalf("exec wrapped: %v", err)
	}
	if string(res.Stdout) == "" {
		t.Fatal("expected stdout from echo under bwrap")
	}
}
