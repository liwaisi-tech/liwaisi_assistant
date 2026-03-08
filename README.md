# liwaisi_assistant

A custom optimized AI assistant for the liwaisi workday in Liwaisi.

## Installation

### Quick install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/liwaisi-tech/liwaisi_assistant/main/scripts/install.sh | bash
```

To install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/liwaisi-tech/liwaisi_assistant/main/scripts/install.sh | bash -s -- --version 0.1.0-beta.1
```

The script installs the `liwaisi` binary to `$HOME/.local/bin`. Make sure that directory is in your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Manual download

Download the archive for your platform from [GitHub Releases](https://github.com/liwaisi-tech/liwaisi_assistant/releases), extract it, and place the `liwaisi` binary somewhere on your `PATH`.

### Build from source

Requires Go 1.24+.

```bash
git clone https://github.com/liwaisi-tech/liwaisi_assistant.git
cd liwaisi_assistant/back/go-assistant
make build-cli
# Binary is at bin/liwaisi
```

## Usage

### Interactive chat
Start a persistent chat session with the assistant:

```bash
liwaisi chat
```

### Single query
Run a one-off query without entering an interactive session:

```bash
liwaisi ask "What is the capital of France?"
```

### Built-in Planner
The `plan` command allows you to execute complex, multi-step tasks by decomposing them into a Directed Acyclic Graph (DAG) of micro-tasks. Independent tasks are executed in parallel, while dependent tasks are run sequentially.

```bash
# Simple tasks bypass LLM decomposition
liwaisi plan "say hello"

# Complex tasks are decomposed into a parallel plan
liwaisi plan "research the 3 best Go concurrency patterns and compare them"

# Use --fail-fast to stop immediately if any task fails
liwaisi plan "..." --fail-fast
```

The planner is also available as a built-in subagent that the main assistant can delegate tasks to.

## Versioning

This project follows [Semantic Versioning 2.0.0](https://semver.org/). During early development, releases use pre-release identifiers (e.g. `v0.1.0-beta.1`) to signal that APIs and capabilities may change.

Version metadata is embedded at compile time via Go's `-ldflags` and includes:

- **Version** — the semantic version tag
- **Git commit** — the short SHA of the commit the binary was built from
- **Build time** — UTC timestamp of the build

Run `liwaisi version` to see full build metadata.

## Release Process

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions.

1. Create and push a semver tag:
   ```bash
   git tag v0.1.0-beta.1
   git push origin v0.1.0-beta.1
   ```
2. The `Release` workflow triggers automatically, builds cross-platform binaries (linux/darwin x amd64/arm64), and creates a GitHub Release with archives and checksums.
3. Pre-release tags (containing `-beta`, `-alpha`, `-rc`, etc.) are automatically marked as pre-releases on GitHub.
