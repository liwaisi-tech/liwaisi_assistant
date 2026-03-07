# go-assistant

A Go microservice and CLI application built with hexagonal architecture (ports and adapters), providing a health check API backed by SQLite and an interactive terminal UI for AI agent interaction.

## Architecture

```
                    ┌─────────────────────────────────────────────┐
                    │              Application Core                │
                    │                                             │
  ┌──────────┐     │  ┌─────────┐     ┌──────────────────────┐   │     ┌───────────┐
  │  HTTP     │────▶│  │  Input  │────▶│  Application Service │   │     │  SQLite   │
  │  (Echo)   │     │  │  Port   │     │  (Health Service)    │   │     │  Database  │
  └──────────┘     │  │         │     │                      │──▶│────▶│           │
                    │  │         │     │                      │   │     └───────────┘
  ┌──────────┐     │  │         │     │                      │   │
  │  CLI/TUI │────▶│  │         │────▶│                      │   │
  │  (Cobra  │     │  └─────────┘     └──────────────────────┘   │
  │  +Bubble │     │                                             │
  │   Tea)   │     │              Domain Entities                │
  └──────────┘     │              & Value Objects                │
                    │                                             │
  Driving Side      └─────────────────────────────────────────────┘
  (Adapters)                                                    Driven Side
```

**Layers:**

- **Domain** (`internal/domain/`) - Entities, value objects, and port interfaces
- **Application** (`internal/application/`) - Use case orchestration (services)
- **Infrastructure** (`internal/infrastructure/`) - Adapters (HTTP handlers, CLI/TUI, SQLite persistence)
- **Config** (`configs/`) - Environment-based configuration
- **API** (`pkg/response/`) - Shared response structs

## Prerequisites

- Go 1.24+
- golangci-lint v1.64+
- Docker and Docker Compose (for containerized runs)
- goimports (`go install golang.org/x/tools/cmd/goimports@latest`)

## Quick Start

```bash
cd back/go-assistant

# Download dependencies
make deps

# Run the HTTP server locally
make run

# Run the CLI locally
make run-cli
```

The server starts on `http://localhost:8080`.

## CLI (`liwaisi`)

The `liwaisi` CLI is the primary interactive interface for the agent. It provides an interactive REPL with markdown rendering, theming, and session management.

### Build and Run

```bash
make build-cli      # builds bin/liwaisi
./bin/liwaisi --help   # show all commands
```

### Commands

| Command | Description |
|---------|-------------|
| `liwaisi chat` | Start an interactive full-screen chat session |
| `liwaisi ask <query>` | Send a single query and print the response |
| `liwaisi health` | Check backend service health status |
| `liwaisi config show` | Display current configuration |
| `liwaisi config set <key> <value>` | Set a configuration value |
| `liwaisi version` | Print version, Go version, and OS info |

### Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--theme` | `dark` | Color theme (`dark`, `light`) |
| `-v`, `--verbose` | `false` | Enable verbose output |

### Key Bindings (chat mode)

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Up/Down` | Cycle input history |
| `Ctrl+C` | Cancel current generation / exit |
| `Ctrl+D` / `Esc` | Exit |

### Configuration

Configuration is stored at `$HOME/.liwaisi/config/liwaisi.yaml` (override root with `BRAE_HOME`).

| Key | Default | Env Override | Description |
|-----|---------|--------------|-------------|
| `theme` | `dark` | `GO_ASSISTANT_CLI_THEME` | Color theme |
| `history_size` | `100` | `GO_ASSISTANT_CLI_HISTORY_SIZE` | Input history buffer size |

### Liwaisi Home Directory

The CLI uses `$HOME/.liwaisi/` as its runtime directory (overridable via `BRAE_HOME`):

```
$HOME/.liwaisi/
├── boot/       — startup config, profile
├── config/     — user settings (liwaisi.yaml)
├── workspace/  — per-project contexts and history
├── data/       — persistent storage (SQLite)
├── bin/        — plugins, tools
└── tmp/        — ephemeral scratch (cleaned on startup)
```

## Available Make Targets

| Target            | Description                                 |
|-------------------|---------------------------------------------|
| `make build`      | Build server binary to `bin/go-assistant`   |
| `make build-cli`  | Build CLI binary to `bin/liwaisi`              |
| `make test`       | Run tests with race detection and coverage  |
| `make test-coverage` | Generate HTML coverage report            |
| `make lint`       | Run golangci-lint                           |
| `make fmt`        | Format code with gofmt and goimports        |
| `make run`        | Run the server locally                      |
| `make run-cli`    | Run the CLI locally                         |
| `make clean`      | Remove build artifacts and coverage files   |
| `make deps`       | Download and tidy dependencies              |
| `make docker-build` | Build Docker image via docker compose     |
| `make docker-up`  | Start containers in detached mode           |
| `make docker-down`| Stop and remove containers                  |
| `make help`       | Show all available targets                  |

## Testing

```bash
# Run all tests
make test

# Generate coverage report
make test-coverage
# Open coverage.html in your browser
```

## Docker

```bash
# Build the image
make docker-build

# Start the service
make docker-up

# Check health
curl http://localhost:8080/api/v1/health

# Stop the service
make docker-down
```

## Project Structure

```
back/go-assistant/
├── api/                        # API specifications
├── cmd/
│   ├── server/                 # HTTP server entry point
│   └── cli/                    # CLI entry point (liwaisi)
├── configs/
│   ├── config.go               # Server configuration
│   └── cli_config.go           # CLI configuration (YAML + env)
├── deployments/
│   └── docker/
│       └── Dockerfile          # Multi-stage Docker build
├── internal/
│   ├── application/
│   │   └── service/            # Use case implementations
│   ├── domain/
│   │   ├── entity/             # Domain entities
│   │   ├── port/
│   │   │   ├── input/          # Driving ports (services)
│   │   │   └── output/         # Driven ports (repositories)
│   │   └── valueobject/        # Domain value objects
│   └── infrastructure/
│       ├── driven/
│       │   └── persistence/
│       │       └── sqlite/     # SQLite adapter (otelsql instrumented)
│       ├── driving/
│       │   ├── cli/            # CLI driving adapter
│       │   │   ├── app.go      # Root BubbleTea model
│       │   │   ├── commands/   # Cobra command definitions
│       │   │   ├── components/ # BubbleTea sub-models
│       │   │   │   ├── chatview/   # Chat message viewport
│       │   │   │   ├── input/      # Multiline text input
│       │   │   │   ├── spinner/    # Thinking indicator
│       │   │   │   └── statusbar/  # Bottom status bar
│       │   │   ├── home/       # $HOME/.liwaisi/ directory manager
│       │   │   ├── render/     # Markdown + streaming renderer
│       │   │   └── theme/      # LipGloss theme system
│       │   └── http/
│       │       ├── handler/    # HTTP handlers
│       │       ├── middleware/ # Logging, recovery, metrics
│       │       └── router/     # Route definitions (otelecho)
│       └── telemetry/          # OTel SDK bootstrap & lifecycle
├── pkg/
│   └── response/               # Shared response types
├── scripts/
│   ├── migrate.go              # Database migration runner
│   └── seed.go                 # Sample data seeder
├── .golangci.yml               # Linter configuration
├── Makefile                    # Build and dev automation
├── go.mod
└── go.sum
```

## API Documentation

### Health Check

**Endpoint:** `GET /api/v1/health`

Returns the health status of the service and its dependencies.

**Response (200 OK):**

```json
{
  "status": "UP",
  "version": "1.0.0",
  "timestamp": "2026-03-03T12:00:00Z",
  "checks": {
    "database": "UP"
  }
}
```

**Response (503 Service Unavailable):**

```json
{
  "status": "DOWN"
}
```

## Observability

The service includes a full OpenTelemetry observability stack with distributed tracing, structured logging with trace correlation, and metrics collection. The tracing backend is **Jaeger v2**, which is built on the OpenTelemetry Collector framework.

### Local Development Stack

Start all services (go-assistant, Jaeger v2, Prometheus):

```bash
make docker-up
```

| Service    | URL                          | Purpose                          |
|------------|------------------------------|----------------------------------|
| Jaeger UI  | http://localhost:16686        | Trace search, timeline, SPM     |
| Prometheus | http://localhost:9090         | Metrics queries                  |
| go-assistant | http://localhost:8080       | Application API                  |

### What is Instrumented

- **HTTP requests**: Auto-traced via `otelecho` middleware (spans, duration histograms, W3C propagation)
- **Database queries**: Traced via `otelsql` (query spans, connection pool metrics)
- **Logs**: Trace-correlated via `slog.InfoContext` + `otelslog` bridge (`trace_id`/`span_id` in every log line). **Note:** OTel logs are exported via OTLP but the current Jaeger dev config does not include a logs pipeline, so exported logs are not stored. Trace-correlated logs with `trace_id`/`span_id` are always visible on stdout via the JSON handler.
- **Go runtime**: Goroutine count, heap allocation, GC pause (via `runtime.Start()`)
- **Business metrics**: `health_check.total`, `health_check.duration`, `health_check.status`
- **Jaeger SPM**: RED metrics (Request rate, Error rate, Duration) in the Jaeger Monitor tab

## Environment Variables

### Application

| Variable                      | Default                | Description                      |
|-------------------------------|------------------------|----------------------------------|
| `GO_ASSISTANT_SERVER_PORT`    | `8080`                 | TCP port for the HTTP server     |
| `GO_ASSISTANT_DATABASE_PATH`  | `data/go-assistant.db` | File path to the SQLite database |

### Telemetry

| Variable                              | Default         | Description                                      |
|---------------------------------------|-----------------|--------------------------------------------------|
| `GO_ASSISTANT_OTEL_ENABLED`           | `true`          | Enable/disable OTel SDK initialization           |
| `GO_ASSISTANT_OTEL_SERVICE_NAME`      | `go-assistant`  | Service name reported to tracing backend         |
| `GO_ASSISTANT_OTEL_TRACE_SAMPLE_RATIO`| `1.0`           | Head-based sampling probability (0.0-1.0)        |
| `GO_ASSISTANT_ENVIRONMENT`            | `development`   | Deployment environment name                      |
| `GO_ASSISTANT_OTEL_METRIC_INTERVAL_SEC`| `15`           | Periodic metric export interval in seconds       |

### Standard OTel Environment Variables (read automatically by the SDK)

| Variable                        | Example                  | Description                |
|---------------------------------|--------------------------|----------------------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT`  | `http://jaeger:4317`     | Jaeger v2 OTLP endpoint   |
| `OTEL_EXPORTER_OTLP_PROTOCOL`  | `grpc`                   | Transport protocol         |
| `OTEL_EXPORTER_OTLP_HEADERS`   | `Authorization=Bearer x` | Auth headers               |
| `OTEL_EXPORTER_OTLP_TIMEOUT`   | `10000`                  | Export timeout (ms)        |

## Database Scripts

```bash
# Run migrations (creates tables)
go run scripts/migrate.go

# Insert sample data
go run scripts/seed.go
```

Set `GO_ASSISTANT_DATABASE_PATH` to customize the database location.
