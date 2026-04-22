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

# Base toolbelt for the `brae` agent. HostGate policy (default.yaml) is the
# authoritative boundary for what can run; these packages only make the
# policy's safe/caution bands actually executable.
#   - curl: healthcheck + caution-gated outbound (wget is removed below)
#   - git/make/jq: devops + build baseline
#   - bash/coreutils/grep/sed/findutils: POSIX surface for safe_patterns
#   - ca-certificates/openssl/gnupg: TLS + signature verification
#   - tzdata/tini: correct timestamps + PID 1 reaping
RUN apk add --no-cache \
        bash coreutils findutils grep sed gawk procps \
        ca-certificates openssl gnupg \
        curl git make jq tzdata tini \
    && rm -f /usr/bin/wget /bin/wget

# Recent Go toolchain for brae (build/test/lint its own stack).
# Pinned to the same minor the server was built with.
ARG GO_VERSION=1.25.1
ARG TARGETARCH=amd64
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
        -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt

# Security: non-root user with a real $HOME it fully owns.
# The brae agent's sandbox is OS-level: kernel fs permissions on this user
# are the real boundary. AllowedRoot defaults to this $HOME.
#
# `-s /bin/bash` makes bash the login shell (adduser's default is /bin/sh).
# The `brae-*` subdirs below mirror the places resolveUserPath / tool-forge
# expect to exist; creating them up front avoids "ENOENT" on first use.
RUN addgroup -S brae \
    && adduser -S -D -H -s /bin/bash -h /home/brae -G brae brae \
    && mkdir -p \
        /home/brae/workspace \
        /home/brae/bin \
        /home/brae/go \
        /home/brae/Documents \
        /home/brae/Notes \
        /home/brae/Music \
        /home/brae/Images \
        /home/brae/Videos \
        /home/brae/Downloads \
        /home/brae/.cache \
        /home/brae/.brae/tmp \
    && chown -R brae:brae /home/brae \
    && chmod 0755 /home/brae

# brae's shell configuration. BASH_ENV below makes bash source ~/.bashrc
# even for non-interactive `bash -c '...'` invocations — that is how the
# LLM-emitted commands inherit PATH, HOME, and locale.
COPY --chown=brae:brae infra/host/brae/bashrc       /home/brae/.bashrc
COPY --chown=brae:brae infra/host/brae/profile      /home/brae/.bash_profile
COPY --chown=brae:brae infra/host/brae/LAYOUT.md    /home/brae/LAYOUT.md
COPY --chown=brae:brae infra/host/brae/layout.json  /home/brae/.brae/layout.json

COPY --from=builder /liwaisi-server /usr/local/bin/liwaisi-server
COPY --from=builder /build/store/postgres/migrations /app/migrations
COPY --from=builder /build/infra/host/policies /app/infra/host/policies

ENV HOST_POLICY_PATH=/app/infra/host/policies/default.yaml
ENV HOME=/home/brae
ENV GOPATH=/home/brae/go
ENV GOCACHE=/home/brae/.cache/go-build
ENV BASH_ENV=/home/brae/.bashrc
ENV ENV=/home/brae/.bashrc
ENV SHELL=/bin/bash
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PATH=/home/brae/bin:/home/brae/go/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
WORKDIR /app

# Ensure secrets directory exists and is writable by app user
RUN mkdir -p /data/secrets && chown brae:brae /data/secrets && chmod 0700 /data/secrets

USER brae

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/api/v1/health || exit 1

ENTRYPOINT ["/sbin/tini", "--", "liwaisi-server"]
