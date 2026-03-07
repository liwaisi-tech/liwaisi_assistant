// Package configs provides configuration loading for the go-assistant service.
package configs

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	// ServerPort is the TCP port the HTTP server listens on.
	ServerPort int
	// DatabasePath is the file path to the SQLite database.
	DatabasePath string
	// Telemetry holds the OpenTelemetry configuration.
	Telemetry TelemetryConfig
}

// TelemetryConfig holds the OpenTelemetry observability configuration.
type TelemetryConfig struct {
	// Enabled controls whether the OTel SDK is initialized.
	Enabled bool
	// ServiceName is the logical name of this service reported to the tracing backend.
	ServiceName string
	// TraceSampleRatio is the head-based sampling probability (0.0–1.0).
	TraceSampleRatio float64
	// Environment is the deployment environment name (e.g. "development", "production").
	Environment string
	// MetricInterval is the periodic metric export interval in seconds.
	MetricInterval int
}

// Load reads configuration from environment variables with the prefix GO_ASSISTANT.
func Load() (*Config, error) {
	cfg := Config{
		ServerPort:   8080,
		DatabasePath: "data/go-assistant.db",
		Telemetry: TelemetryConfig{
			Enabled:          true,
			ServiceName:      "go-assistant",
			TraceSampleRatio: 1.0,
			Environment:      "development",
			MetricInterval:   15,
		},
	}

	if v := os.Getenv("GO_ASSISTANT_SERVER_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing GO_ASSISTANT_SERVER_PORT: %w", err)
		}
		cfg.ServerPort = port
	}

	if v := os.Getenv("GO_ASSISTANT_DATABASE_PATH"); v != "" {
		cfg.DatabasePath = v
	}

	if v := os.Getenv("GO_ASSISTANT_OTEL_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("parsing GO_ASSISTANT_OTEL_ENABLED: %w", err)
		}
		cfg.Telemetry.Enabled = b
	}

	if v := os.Getenv("GO_ASSISTANT_OTEL_SERVICE_NAME"); v != "" {
		cfg.Telemetry.ServiceName = v
	}

	if v := os.Getenv("GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO"); v != "" {
		r, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO: %w", err)
		}
		if r < 0.0 || r > 1.0 {
			return nil, fmt.Errorf("GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO must be in [0.0, 1.0], got %f", r)
		}
		cfg.Telemetry.TraceSampleRatio = r
	}

	if v := os.Getenv("GO_ASSISTANT_ENVIRONMENT"); v != "" {
		cfg.Telemetry.Environment = v
	}

	if v := os.Getenv("GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("parsing GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC: %w", err)
		}
		if i <= 0 {
			return nil, fmt.Errorf("GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC must be positive, got %d", i)
		}
		cfg.Telemetry.MetricInterval = i
	}

	return &cfg, nil
}
