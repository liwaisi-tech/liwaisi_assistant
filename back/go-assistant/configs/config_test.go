package configs

import (
	"os"
	"testing"
)

var configEnvVars = []string{
	"GO_ASSISTANT_SERVER_PORT",
	"GO_ASSISTANT_DATABASE_PATH",
	"GO_ASSISTANT_OTEL_ENABLED",
	"GO_ASSISTANT_OTEL_SERVICE_NAME",
	"GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO",
	"GO_ASSISTANT_ENVIRONMENT",
	"GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range configEnvVars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name         string
		envVars      map[string]string
		expectedPort int
		expectedDB   string
	}{
		{
			name:         "default values when no env vars set",
			envVars:      nil,
			expectedPort: 8080,
			expectedDB:   "data/go-assistant.db",
		},
		{
			name: "custom values from environment",
			envVars: map[string]string{
				"GO_ASSISTANT_SERVER_PORT":   "9090",
				"GO_ASSISTANT_DATABASE_PATH": "/tmp/test.db",
			},
			expectedPort: 9090,
			expectedDB:   "/tmp/test.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)

			for k, v := range tt.envVars {
				os.Setenv(k, v)
				t.Cleanup(func() { os.Unsetenv(k) })
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load returned unexpected error: %v", err)
			}

			if cfg.ServerPort != tt.expectedPort {
				t.Errorf("ServerPort = %d, want %d", cfg.ServerPort, tt.expectedPort)
			}

			if cfg.DatabasePath != tt.expectedDB {
				t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, tt.expectedDB)
			}
		})
	}
}

func TestLoad_TelemetryDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	if !cfg.Telemetry.Enabled {
		t.Error("Telemetry.Enabled should default to true")
	}
	if cfg.Telemetry.ServiceName != "go-assistant" {
		t.Errorf("Telemetry.ServiceName = %q, want %q", cfg.Telemetry.ServiceName, "go-assistant")
	}
	if cfg.Telemetry.TraceSampleRatio != 1.0 {
		t.Errorf("Telemetry.TraceSampleRatio = %f, want %f", cfg.Telemetry.TraceSampleRatio, 1.0)
	}
	if cfg.Telemetry.Environment != "development" {
		t.Errorf("Telemetry.Environment = %q, want %q", cfg.Telemetry.Environment, "development")
	}
	if cfg.Telemetry.MetricInterval != 15 {
		t.Errorf("Telemetry.MetricInterval = %d, want %d", cfg.Telemetry.MetricInterval, 15)
	}
}

func TestLoad_TelemetryCustom(t *testing.T) {
	clearEnv(t)

	envVars := map[string]string{
		"GO_ASSISTANT_OTEL_ENABLED":             "false",
		"GO_ASSISTANT_OTEL_SERVICE_NAME":        "custom-svc",
		"GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO":  "0.5",
		"GO_ASSISTANT_ENVIRONMENT":              "production",
		"GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC": "30",
	}
	for k, v := range envVars {
		os.Setenv(k, v)
		t.Cleanup(func() { os.Unsetenv(k) })
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	if cfg.Telemetry.Enabled {
		t.Error("Telemetry.Enabled should be false")
	}
	if cfg.Telemetry.ServiceName != "custom-svc" {
		t.Errorf("Telemetry.ServiceName = %q, want %q", cfg.Telemetry.ServiceName, "custom-svc")
	}
	if cfg.Telemetry.TraceSampleRatio != 0.5 {
		t.Errorf("Telemetry.TraceSampleRatio = %f, want %f", cfg.Telemetry.TraceSampleRatio, 0.5)
	}
	if cfg.Telemetry.Environment != "production" {
		t.Errorf("Telemetry.Environment = %q, want %q", cfg.Telemetry.Environment, "production")
	}
	if cfg.Telemetry.MetricInterval != 30 {
		t.Errorf("Telemetry.MetricInterval = %d, want %d", cfg.Telemetry.MetricInterval, 30)
	}
}

func TestLoad_TelemetryInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
	}{
		{
			name:    "invalid OTEL_ENABLED",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_ENABLED": "not-a-bool"},
		},
		{
			name:    "invalid TRACE_SAMPLE_RATIO",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO": "abc"},
		},
		{
			name:    "invalid METRIC_INTERVAL_SEC",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC": "xyz"},
		},
		{
			name:    "out of range TRACE_SAMPLE_RATIO",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO": "2.0"},
		},
		{
			name:    "negative TRACE_SAMPLE_RATIO",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO": "-0.5"},
		},
		{
			name:    "zero METRIC_INTERVAL_SEC",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC": "0"},
		},
		{
			name:    "negative METRIC_INTERVAL_SEC",
			envVars: map[string]string{"GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC": "-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)

			for k, v := range tt.envVars {
				os.Setenv(k, v)
				t.Cleanup(func() { os.Unsetenv(k) })
			}

			_, err := Load()
			if err == nil {
				t.Fatal("Load should have returned an error")
			}
		})
	}
}
