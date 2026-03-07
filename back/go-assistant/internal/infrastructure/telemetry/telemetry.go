package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
)

// Shutdown holds cleanup functions for graceful telemetry shutdown.
type Shutdown struct {
	funcs []func(ctx context.Context) error
}

// Add registers a cleanup function to be called during shutdown.
func (s *Shutdown) Add(fn func(ctx context.Context) error) {
	s.funcs = append(s.funcs, fn)
}

// Execute runs all registered cleanup functions and returns combined errors.
func (s *Shutdown) Execute(ctx context.Context) error {
	var errs []error
	for _, fn := range s.funcs {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Init initializes the OpenTelemetry SDK (traces, metrics, logs) and configures
// the global slog default with a fan-out handler that writes to both stdout (JSON)
// and the OTel log pipeline.
//
// When cfg.Telemetry.Enabled is false, Init is a no-op and returns an empty Shutdown.
func Init(ctx context.Context, cfg *configs.Config, serviceVersion string) (*Shutdown, error) {
	shutdown := &Shutdown{}

	if !cfg.Telemetry.Enabled {
		return shutdown, nil
	}

	res, err := newResource(cfg.Telemetry.ServiceName, serviceVersion, cfg.Telemetry.Environment)
	if err != nil {
		return nil, fmt.Errorf("creating otel resource: %w", err)
	}

	tp, err := newTracerProvider(ctx, res, cfg.Telemetry.TraceSampleRatio)
	if err != nil {
		return nil, fmt.Errorf("creating tracer provider: %w", err)
	}
	shutdown.Add(tp.Shutdown)

	mp, err := newMeterProvider(ctx, res, time.Duration(cfg.Telemetry.MetricInterval)*time.Second)
	if err != nil {
		return nil, fmt.Errorf("creating meter provider: %w", err)
	}
	shutdown.Add(mp.Shutdown)

	lp, err := newLoggerProvider(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("creating logger provider: %w", err)
	}
	shutdown.Add(lp.Shutdown)

	otelHandler := otelslog.NewHandler(cfg.Telemetry.ServiceName, otelslog.WithLoggerProvider(lp))
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(NewFanOutHandler(jsonHandler, otelHandler)))

	return shutdown, nil
}

func newResource(serviceName, serviceVersion, environment string) (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			semconv.DeploymentEnvironmentName(environment),
		),
	)
}

func newTracerProvider(ctx context.Context, res *resource.Resource, sampleRatio float64) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

func newMeterProvider(ctx context.Context, res *resource.Resource, interval time.Duration) (*sdkmetric.MeterProvider, error) {
	exporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return mp, nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	exporter, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating log exporter: %w", err)
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)

	return lp, nil
}

// CLIConfig holds configuration for CLI telemetry initialization.
type CLIConfig struct {
	// LogDir is the directory where log files are written.
	LogDir string
	// ServiceVersion is reported in OTel resource attributes.
	ServiceVersion string
}

// InitCLI sets up structured logging for the CLI, writing JSON logs to a file
// in cfg.LogDir. OpenTelemetry is optionally initialized when the
// GO_ASSISTANT_OTEL_ENABLED environment variable is set to true.
//
// The returned LevelVar allows callers to adjust the log level dynamically
// (e.g. when the --verbose flag is parsed after startup).
func InitCLI(ctx context.Context, cfg CLIConfig) (*Shutdown, *slog.LevelVar, error) {
	shutdown := &Shutdown{}
	level := &slog.LevelVar{}
	level.Set(slog.LevelInfo)

	if err := os.MkdirAll(cfg.LogDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(
		filepath.Join(cfg.LogDir, "liwaisi.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o640,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file: %w", err)
	}
	shutdown.Add(func(context.Context) error { return logFile.Close() })

	fileHandler := slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: level})

	otelEnabled, _ := strconv.ParseBool(os.Getenv("GO_ASSISTANT_OTEL_ENABLED"))
	if !otelEnabled {
		slog.SetDefault(slog.New(fileHandler))
		return shutdown, level, nil
	}

	svcName := envOrDefault("GO_ASSISTANT_OTEL_SERVICE_NAME", "liwaisi-cli")
	env := envOrDefault("GO_ASSISTANT_ENVIRONMENT", "development")
	sampleRatio := envFloatOrDefault("GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO", 1.0)
	metricInterval := envIntOrDefault("GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC", 15)

	res, err := newResource(svcName, cfg.ServiceVersion, env)
	if err != nil {
		return nil, nil, fmt.Errorf("creating otel resource: %w", err)
	}

	tp, err := newTracerProvider(ctx, res, sampleRatio)
	if err != nil {
		return nil, nil, fmt.Errorf("creating tracer provider: %w", err)
	}
	shutdown.Add(tp.Shutdown)

	mp, err := newMeterProvider(ctx, res, time.Duration(metricInterval)*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("creating meter provider: %w", err)
	}
	shutdown.Add(mp.Shutdown)

	lp, err := newLoggerProvider(ctx, res)
	if err != nil {
		return nil, nil, fmt.Errorf("creating logger provider: %w", err)
	}
	shutdown.Add(lp.Shutdown)

	otelHandler := otelslog.NewHandler(svcName, otelslog.WithLoggerProvider(lp))
	slog.SetDefault(slog.New(NewFanOutHandler(fileHandler, otelHandler)))

	return shutdown, level, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envFloatOrDefault(key string, defaultVal float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultVal
	}
	return f
}

func envIntOrDefault(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(v)
	if err != nil || i <= 0 {
		return defaultVal
	}
	return i
}
