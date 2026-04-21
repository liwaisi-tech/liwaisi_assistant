# ── Build stage ──
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache dependencies (stdlib-only, no go.sum)
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary with version info
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.Version=${VERSION} \
              -X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.GitCommit=${COMMIT} \
              -X github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version.BuildTime=${BUILT}" \
    -o /liwaisi-server ./cmd/server

# ── Runtime stage ──
FROM alpine:3.21

# Security: non-root user
# Alpine 3.21 ships with ca-certificates pre-installed
RUN addgroup -S brae && adduser -S brae -G brae

COPY --from=builder /liwaisi-server /usr/local/bin/liwaisi-server
COPY --from=builder /build/store/postgres/migrations /app/migrations
COPY --from=builder /build/infra/host/policies /app/infra/host/policies

ENV HOST_POLICY_PATH=/app/infra/host/policies/default.yaml
WORKDIR /app

# Ensure secrets directory exists and is writable by app user
RUN mkdir -p /data/secrets && chown brae:brae /data/secrets && chmod 0700 /data/secrets

USER brae

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["liwaisi-server"]
