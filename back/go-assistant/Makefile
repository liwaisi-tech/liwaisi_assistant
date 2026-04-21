BINARY_NAME := liwaisi
BIN_DIR     := bin
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html
GO          := go
GO_FLAGS    := -race
LINT        := golangci-lint
VERSION     ?= dev
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version
LDFLAGS     := -s -w \
    -X $(VERSION_PKG).Version=$(VERSION) \
    -X $(VERSION_PKG).GitCommit=$(GIT_COMMIT) \
    -X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

.DEFAULT_GOAL := help

.PHONY: build build-server test test-coverage test-integration e2e bench bench-gate lint fmt clean deps run help

## build: Build the CLI binary
build:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/cli

## build-server: Build the HTTP server binary
build-server:
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-server ./cmd/server

## run: Run the HTTP server (dev mode)
run: build-server
	./$(BIN_DIR)/$(BINARY_NAME)-server

## test: Run all tests with race detection
test:
	$(GO) test $(GO_FLAGS) -cover -count=1 ./...

## test-coverage: Generate coverage report (used by CI)
test-coverage:
	$(GO) test $(GO_FLAGS) -coverprofile=$(COVERAGE_FILE) -count=1 ./...
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)

## test-integration: Run integration tests (requires OPENROUTER_API_KEY)
test-integration:
	$(GO) test $(GO_FLAGS) -tags integration -timeout 300s -count=1 -v ./integration/

## e2e: Run SC-17 end-to-end harness (requires docker + docker compose)
e2e:
	$(GO) test -tags=e2e -timeout 300s -count=1 -v ./integration/...

## bench: Run SC-17 latency benchmarks and capture output
bench:
	@mkdir -p bench
	$(GO) test -bench='BenchmarkProbeFanoutCompose|BenchmarkAwakeningE2E' -run=^$$ -benchmem -benchtime=5x \
		./cpn/awakens/fanout/... ./internal/app/... | tee bench/last.txt

## bench-gate: Compare latest bench run against committed baseline (>10%% fails)
bench-gate: bench
	./scripts/benchstat_gate.sh bench/awakening_baseline.txt bench/last.txt

## lint: Run golangci-lint
lint:
	$(LINT) run

## fmt: Format code
fmt:
	gofmt -w .

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)/
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)

## deps: Download and tidy dependencies (used by CI)
deps:
	$(GO) mod download
	$(GO) mod tidy

## help: Show available targets
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
