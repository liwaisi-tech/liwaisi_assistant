package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *configs.Config
		wantErr bool
	}{
		{
			name: "disabled telemetry returns empty shutdown without error",
			cfg: &configs.Config{
				Telemetry: configs.TelemetryConfig{
					Enabled: false,
				},
			},
			wantErr: false,
		},
		{
			name: "enabled telemetry initializes providers",
			cfg: &configs.Config{
				Telemetry: configs.TelemetryConfig{
					Enabled:          true,
					ServiceName:      "test-service",
					TraceSampleRatio: 1.0,
					Environment:      "test",
					MetricInterval:   1,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Point the OTLP exporter at a non-existent endpoint so it doesn't
			// block. The SDK connects lazily; Init should still succeed.
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:0")

			ctx := context.Background()
			shutdown, err := Init(ctx, tt.cfg, "0.0.0-test")

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if shutdown == nil {
				t.Fatal("shutdown should never be nil")
			}

			// Use a short-lived context for shutdown: no collector is running,
			// so the metric exporter will fail to flush — that is expected.
			shutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			// Errors during shutdown in tests are expected (no collector).
			_ = shutdown.Execute(shutCtx)
		})
	}
}

func TestShutdown_Execute(t *testing.T) {
	errBoom := errors.New("boom")
	errCrash := errors.New("crash")

	tests := []struct {
		name      string
		funcs     []func(context.Context) error
		wantErr   bool
		wantCount int
	}{
		{
			name:      "empty shutdown succeeds",
			funcs:     nil,
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "all succeed",
			funcs: []func(context.Context) error{
				func(context.Context) error { return nil },
				func(context.Context) error { return nil },
			},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name: "collects all errors",
			funcs: []func(context.Context) error{
				func(context.Context) error { return errBoom },
				func(context.Context) error { return nil },
				func(context.Context) error { return errCrash },
			},
			wantErr:   true,
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Shutdown{}
			for _, fn := range tt.funcs {
				s.Add(fn)
			}

			err := s.Execute(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantErr {
				var joined interface{ Unwrap() []error }
				if errors.As(err, &joined) {
					if got := len(joined.Unwrap()); got != tt.wantCount {
						t.Errorf("error count = %d, want %d", got, tt.wantCount)
					}
				}
			}
		})
	}
}

func TestFanOutHandler(t *testing.T) {
	t.Run("Enabled returns true if any handler is enabled", func(t *testing.T) {
		debug := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelDebug})
		warn := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})

		h := NewFanOutHandler(debug, warn)
		if !h.Enabled(context.Background(), slog.LevelInfo) {
			t.Error("expected Enabled=true for LevelInfo (debug handler accepts it)")
		}
	})

	t.Run("Enabled returns false when no handler accepts level", func(t *testing.T) {
		warn := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})
		errH := slog.NewJSONHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})

		h := NewFanOutHandler(warn, errH)
		if h.Enabled(context.Background(), slog.LevelInfo) {
			t.Error("expected Enabled=false for LevelInfo")
		}
	})

	t.Run("Handle fans out to all enabled handlers", func(t *testing.T) {
		var buf1, buf2 bytes.Buffer
		h1 := slog.NewJSONHandler(&buf1, &slog.HandlerOptions{Level: slog.LevelInfo})
		h2 := slog.NewJSONHandler(&buf2, &slog.HandlerOptions{Level: slog.LevelInfo})

		fan := NewFanOutHandler(h1, h2)
		logger := slog.New(fan)
		logger.Info("test message")

		if buf1.Len() == 0 {
			t.Error("handler 1 received no output")
		}
		if buf2.Len() == 0 {
			t.Error("handler 2 received no output")
		}
	})

	t.Run("Handle skips disabled handlers", func(t *testing.T) {
		var infoBuf, warnBuf bytes.Buffer
		infoH := slog.NewJSONHandler(&infoBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
		warnH := slog.NewJSONHandler(&warnBuf, &slog.HandlerOptions{Level: slog.LevelWarn})

		fan := NewFanOutHandler(infoH, warnH)
		logger := slog.New(fan)
		logger.Info("info only message")

		if infoBuf.Len() == 0 {
			t.Error("info handler should have received the message")
		}
		if warnBuf.Len() != 0 {
			t.Error("warn handler should not have received an info-level message")
		}
	})

	t.Run("WithAttrs returns new handler", func(t *testing.T) {
		var buf bytes.Buffer
		h := NewFanOutHandler(slog.NewJSONHandler(&buf, nil))
		h2 := h.WithAttrs([]slog.Attr{slog.String("key", "val")})
		if h2 == nil {
			t.Fatal("WithAttrs returned nil")
		}
	})

	t.Run("WithGroup returns new handler", func(t *testing.T) {
		var buf bytes.Buffer
		h := NewFanOutHandler(slog.NewJSONHandler(&buf, nil))
		h2 := h.WithGroup("group")
		if h2 == nil {
			t.Fatal("WithGroup returned nil")
		}
	})
}

func TestInitCLI(t *testing.T) {
	tests := []struct {
		name      string
		otelEnv   string
		wantErr   bool
		wantLevel slog.Level
	}{
		{
			name:      "file-only logging without OTel",
			otelEnv:   "false",
			wantErr:   false,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "file-only logging with OTel disabled by default",
			otelEnv:   "",
			wantErr:   false,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "with OTel enabled",
			otelEnv:   "true",
			wantErr:   false,
			wantLevel: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:0")
			if tt.otelEnv != "" {
				t.Setenv("GO_ASSISTANT_OTEL_ENABLED", tt.otelEnv)
			} else {
				t.Setenv("GO_ASSISTANT_OTEL_ENABLED", "")
			}

			logDir := t.TempDir()
			ctx := context.Background()

			shutdown, level, err := InitCLI(ctx, CLIConfig{
				LogDir:         logDir,
				ServiceVersion: "0.0.0-test",
			})

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if shutdown == nil {
				t.Fatal("shutdown should never be nil")
			}
			if level == nil {
				t.Fatal("level should never be nil")
			}
			if level.Level() != tt.wantLevel {
				t.Errorf("level = %v, want %v", level.Level(), tt.wantLevel)
			}

			logFile := filepath.Join(logDir, "liwaisi.log")
			if _, err := os.Stat(logFile); os.IsNotExist(err) {
				t.Error("expected log file to be created")
			}

			shutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			_ = shutdown.Execute(shutCtx)
		})
	}
}

func TestInitCLI_LevelVarDynamic(t *testing.T) {
	t.Setenv("GO_ASSISTANT_OTEL_ENABLED", "false")

	logDir := t.TempDir()
	shutdown, level, err := InitCLI(context.Background(), CLIConfig{
		LogDir:         logDir,
		ServiceVersion: "0.0.0-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if level.Level() != slog.LevelInfo {
		t.Errorf("initial level = %v, want Info", level.Level())
	}

	level.Set(slog.LevelDebug)
	if level.Level() != slog.LevelDebug {
		t.Errorf("after Set(Debug), level = %v, want Debug", level.Level())
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = shutdown.Execute(shutCtx)
}

func TestInitCLI_InvalidLogDir(t *testing.T) {
	t.Setenv("GO_ASSISTANT_OTEL_ENABLED", "false")

	_, _, err := InitCLI(context.Background(), CLIConfig{
		LogDir:         "/dev/null/impossible",
		ServiceVersion: "0.0.0-test",
	})
	if err == nil {
		t.Fatal("expected error for invalid log directory, got nil")
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Run("envOrDefault returns env value", func(t *testing.T) {
		t.Setenv("TEST_ENV_HELPER", "custom")
		if got := envOrDefault("TEST_ENV_HELPER", "default"); got != "custom" {
			t.Errorf("envOrDefault = %q, want %q", got, "custom")
		}
	})

	t.Run("envOrDefault returns default for empty", func(t *testing.T) {
		t.Setenv("TEST_ENV_HELPER_EMPTY", "")
		if got := envOrDefault("TEST_ENV_HELPER_EMPTY", "fallback"); got != "fallback" {
			t.Errorf("envOrDefault = %q, want %q", got, "fallback")
		}
	})

	t.Run("envFloatOrDefault parses valid float", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "0.5")
		if got := envFloatOrDefault("TEST_FLOAT", 1.0); got != 0.5 {
			t.Errorf("envFloatOrDefault = %v, want 0.5", got)
		}
	})

	t.Run("envFloatOrDefault returns default for invalid", func(t *testing.T) {
		t.Setenv("TEST_FLOAT_BAD", "notanumber")
		if got := envFloatOrDefault("TEST_FLOAT_BAD", 0.75); got != 0.75 {
			t.Errorf("envFloatOrDefault = %v, want 0.75", got)
		}
	})

	t.Run("envIntOrDefault parses valid int", func(t *testing.T) {
		t.Setenv("TEST_INT", "30")
		if got := envIntOrDefault("TEST_INT", 15); got != 30 {
			t.Errorf("envIntOrDefault = %v, want 30", got)
		}
	})

	t.Run("envIntOrDefault returns default for non-positive", func(t *testing.T) {
		t.Setenv("TEST_INT_ZERO", "0")
		if got := envIntOrDefault("TEST_INT_ZERO", 10); got != 10 {
			t.Errorf("envIntOrDefault = %v, want 10", got)
		}
	})
}
