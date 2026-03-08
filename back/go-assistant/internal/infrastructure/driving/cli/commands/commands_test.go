package commands

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

type mockHealthService struct {
	health *entity.Health
	err    error
}

func (m *mockHealthService) GetHealth(_ context.Context) (*entity.Health, error) {
	return m.health, m.err
}

type mockAgentService struct {
	askResponse string
	askErr      error
}

func (m *mockAgentService) Chat(_ context.Context, _ string, _ string) (<-chan valueobject.StreamChunk, error) {
	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "mock response"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockAgentService) Ask(_ context.Context, query string) (string, error) {
	if m.askErr != nil {
		return "", m.askErr
	}
	if m.askResponse != "" {
		return m.askResponse, nil
	}
	return fmt.Sprintf("Answer to: %s", query), nil
}

func newTestOpts(t *testing.T) *Options {
	t.Helper()
	h, err := home.New(t.TempDir())
	if err != nil {
		t.Fatalf("home.New: %v", err)
	}
	if err := h.Init(); err != nil {
		t.Fatalf("home.Init: %v", err)
	}
	return &Options{
		Home: h,
		HealthSvc: &mockHealthService{
			health: &entity.Health{
				Status:  valueobject.StatusUp,
				Version: "test",
				Checks:  map[string]valueobject.Status{"database": valueobject.StatusUp},
			},
		},
		AgentFactory: func() (input.AgentService, string, error) {
			return &mockAgentService{}, "mock-model", nil
		},
	}
}

func TestRootCommand_Help(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root --help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"liwaisi", "version", "health"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	origVersion := version.Version
	origCommit := version.GitCommit
	origBuildTime := version.BuildTime
	t.Cleanup(func() {
		version.Version = origVersion
		version.GitCommit = origCommit
		version.BuildTime = origBuildTime
	})
	version.Version = "test-version"
	version.GitCommit = "abc1234"
	version.BuildTime = "2025-01-01T00:00:00Z"

	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}

	out := buf.String()
	tests := []struct {
		name string
		want string
	}{
		{"version string", "test-version"},
		{"commit", "abc1234"},
		{"build time", "2025-01-01T00:00:00Z"},
		{"go version", runtime.Version()},
		{"os/arch", runtime.GOOS + "/" + runtime.GOARCH},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(out, tt.want) {
				t.Errorf("version output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

func TestHealthCommand_Success(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"health"})

	if err := root.Execute(); err != nil {
		t.Fatalf("health: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"UP", "test", "database"} {
		if !strings.Contains(out, want) {
			t.Errorf("health output missing %q:\n%s", want, out)
		}
	}
}

func TestHealthCommand_Error(t *testing.T) {
	opts := newTestOpts(t)
	opts.HealthSvc = &mockHealthService{err: fmt.Errorf("connection refused")}
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetErr(buf)
	root.SetArgs([]string{"health"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from health command, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain cause, got: %v", err)
	}
}

func TestHealthCommand_Down(t *testing.T) {
	opts := newTestOpts(t)
	opts.HealthSvc = &mockHealthService{
		health: &entity.Health{
			Status:  valueobject.StatusDown,
			Version: "test",
			Checks:  map[string]valueobject.Status{"database": valueobject.StatusDown},
		},
	}
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"health"})

	if err := root.Execute(); err != nil {
		t.Fatalf("health: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "DOWN") {
		t.Errorf("health output should show DOWN:\n%s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("health output should show failure symbol:\n%s", out)
	}
}

func TestConfigShow(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"config", "show"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config show: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"theme:", "history_size:"} {
		if !strings.Contains(out, want) {
			t.Errorf("config show output missing %q:\n%s", want, out)
		}
	}
}

func TestConfigSet_Theme(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"config", "set", "theme", "light"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config set: %v", err)
	}

	root2 := NewRoot(opts)
	buf2 := new(bytes.Buffer)
	root2.SetOut(buf2)
	root2.SetArgs([]string{"config", "show"})

	if err := root2.Execute(); err != nil {
		t.Fatalf("config show: %v", err)
	}

	if !strings.Contains(buf2.String(), "light") {
		t.Errorf("config show should reflect updated theme:\n%s", buf2.String())
	}
}

func TestConfigSet_HistorySize(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"config", "set", "history_size", "200"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config set: %v", err)
	}

	if !strings.Contains(buf.String(), "200") {
		t.Errorf("config set output should confirm value:\n%s", buf.String())
	}
}

func TestConfigSet_InvalidKey(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	root.SetArgs([]string{"config", "set", "bogus", "value"})

	err := root.Execute()
	if err == nil {
		t.Fatal("config set with unknown key should error")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("error should mention unknown key, got: %v", err)
	}
}

func TestAskCommand(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"ask", "what is Go?"})

	if err := root.Execute(); err != nil {
		t.Fatalf("ask: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Answer to") {
		t.Errorf("ask output should contain agent response:\n%s", out)
	}
}

func TestRootCommand_HelpContainsAllCommands(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("root --help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"version", "health", "chat", "ask", "config"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing command %q:\n%s", want, out)
		}
	}
}

func TestChatCommand_Exists(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)

	chatCmd, _, err := root.Find([]string{"chat"})
	if err != nil {
		t.Fatalf("chat command not found: %v", err)
	}
	if chatCmd.Use != "chat" {
		t.Errorf("Use = %q, want chat", chatCmd.Use)
	}
}

func TestConfigSet_InvalidTheme(t *testing.T) {
	opts := newTestOpts(t)
	root := NewRoot(opts)
	root.SetArgs([]string{"config", "set", "theme", "neon"})

	err := root.Execute()
	if err == nil {
		t.Fatal("config set with invalid theme should error")
	}
	if !strings.Contains(err.Error(), "invalid theme") {
		t.Errorf("error should mention invalid theme, got: %v", err)
	}
}
