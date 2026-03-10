# go-assistant Makefile
BINARY_NAME := liwaisi-os
CLI_BINARY_NAME := liwaisi
BIN_DIR := bin
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html
GO := go
GO_FLAGS := -race
LINT := golangci-lint
DOCKER_COMPOSE := docker compose
VERSION ?= dev
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := main
LDFLAGS := -X $(VERSION_PKG).version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: build build-cli test test-coverage lint fmt run run-cli clean deps docker-build docker-up docker-down goreleaser-snapshot help

## build: Build the server binary to bin/liwaisi-os
build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/server

## test: Run all tests with race detection and coverage
test:
	$(GO) test $(GO_FLAGS) -cover -count=1 ./...

## test-coverage: Generate HTML coverage report
test-coverage:
	$(GO) test $(GO_FLAGS) -coverprofile=$(COVERAGE_FILE) -count=1 ./...
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

## lint: Run golangci-lint
lint:
	$(LINT) run

## fmt: Format code with gofmt and goimports
fmt:
	gofmt -w .
	goimports -w .



## run: Run the server locally
run:
	$(GO) run ./cmd/server

## clean: Remove build artifacts and coverage files
clean:
	rm -rf $(BIN_DIR)/
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)

## deps: Download and tidy dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

## docker-build: Build Docker image via docker compose
docker-build:
	$(DOCKER_COMPOSE) -f ../../docker-compose.yml build

## docker-up: Start containers in detached mode
docker-up:
	$(DOCKER_COMPOSE) -f ../../docker-compose.yml up -d

## docker-down: Stop and remove containers
docker-down:
	$(DOCKER_COMPOSE) -f ../../docker-compose.yml down

## goreleaser-snapshot: Test GoReleaser build locally (no publish)
goreleaser-snapshot:
	goreleaser release --snapshot --clean

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
