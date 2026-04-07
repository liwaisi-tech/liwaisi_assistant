# BRAE - Being a Real AI Engineer

A full-stack agentic AI platform built on **Coloured Petri Net (CPN)** theory. BRAE treats every agent, tool, director, and orchestrator as the same universal type -- a CPN -- where identity emerges from topology and role, not from distinct class hierarchies.

## What is a Coloured Petri Net?

A [Coloured Petri Net](https://en.wikipedia.org/wiki/Coloured_Petri_net) is a mathematical formalism for modeling concurrent systems. BRAE adapts this formalism to agentic AI, producing a single execution model that unifies tool calls, LLM reasoning, multi-agent coordination, and human-in-the-loop workflows.

### Core primitives

| Primitive | What it does in BRAE |
|---|---|
| **Place** | A typed buffer that holds tokens. Each place declares a *color* (data type: Human, Text, Tool, Error, Event) and a *space* (Surface or Computation). |
| **Token** | The fundamental data unit flowing through the net. Carries a payload, color, origin CPN ID, depth, and trace ID for full provenance. |
| **Transition** | A unit of computation. Fires when all input places have tokens and an optional guard predicate passes. Transition kinds: `LLM`, `Tool`, `Validate`, `SubNet`, `Observer`, `HITL`. |
| **Arc** | Connects places to transitions (input) and transitions to places (output), defining the data flow topology. |

### How BRAE uses CPN

```
User message
    |
    v
[Surface Place] ---> [LLM Transition] ---> [Computation Place] ---> [Tool Transition] ---> [Surface Place]
  (ColorHuman)         fires goroutine        (ColorText)            fires goroutine        (ColorText)
                       with retry policy                             with circuit breaker
                            |                                               |
                            v                                               v
                    [Error Place] <--- fallback routing <--- [Error Place]
```

**Parallelism is topological.** Concurrency is never declared explicitly -- it emerges from the firing rule: any transition whose input places are satisfied fires in its own goroutine. The executor follows a *consume-before-launch* pattern to prevent race conditions.

**Everything is a CPN.** A single tool call, a multi-step reasoning chain, a team of collaborating agents, and the root orchestrator are all instances of the same `CPN` type at different depths:

| Depth | Role | Example |
|---|---|---|
| 0 | Root Orchestrator | The entry point that receives user messages |
| 1 | Domain Director | A sub-CPN managing a specific domain (research, coding) |
| 2+ | Worker Agent | Leaf CPNs executing specific tasks |
| -1 | Human | The user providing HITL input |

**Sub-CPNs are isolated but observable.** A child CPN cannot read parent places. Instead, it emits events to a shared bus that the parent observes through `Observer` transitions -- making the Observation Space from the theoretical framework concrete.

**Two runtime modes:**
- **MAS (Multi-Agent System)** -- independent firing, agents work autonomously
- **Centaurian** -- co-trigger guards requiring both human-origin and AI-origin tokens before firing, enabling true human-AI collaboration

### Design axioms

The architecture is grounded in ten non-negotiable axioms derived from the [Borghoff, Bottoni & Pareschi (2025)](https://doi.org/10.48550/arXiv.2504.14048) formal framework:

1. **Everything is a CPN** -- identity from topology, not types
2. **Sub-CPNs are isolated** -- own place namespace, no parent access
3. **Sub-CPNs are observable** -- events to shared bus via Observer transitions
4. **HITL is first-class** -- native node kind, blocks branch until human input
5. **Three spaces are structural** -- Surface, Observation, Computation
6. **Tokens carry origin** -- full provenance (CPN ID, depth, kind)
7. **MAS/Centaurian are runtime modes** -- switchable per executor loop iteration
8. **Parallelism is topological** -- emerges from the firing rule
9. **Event store is append-only** -- immutable audit trail
10. **The human channel is the only interface** -- users see only the surface stream

## Architecture overview

```
front/react-assistant/          React 19 + TypeScript + Vite + Tailwind CSS
                                Real-time chat, CPN execution monitor, personality editor

back/go-assistant/              Go 1.25 + Hexagonal Architecture
                                CPN domain core, HTTP API with SSE, PostgreSQL + Redis
```

The backend follows **hexagonal architecture**: the CPN domain core (`cpn/`) has zero external imports. Infrastructure adapters (OpenRouter, PostgreSQL, Redis, Google OAuth) plug in through port interfaces. The HTTP API streams CPN events to the frontend via Server-Sent Events.

### Key capabilities

- **Agentic chat** with streaming LLM responses via SSE
- **CPN execution engine** with topological parallelism, retry policies, and circuit breakers
- **Human-in-the-loop** transitions that block execution until user input
- **Three-layer personality system** (Nucleo/Conducta/Etica) with tension resolution
- **Live execution monitor** with D3 force-directed graph and timeline scrubbing
- **Session forking** to branch conversations from any point in history
- **Multi-model support** via OpenRouter (fallback models, structured output, extended thinking)
- **Cost tracking** with per-session token accounting
- **Internationalization** in Spanish (default) and English

## Quick start

### Docker Compose (recommended)

```bash
cp .env.example .env
# Set OPENROUTER_API_KEY in .env
docker-compose up
```

- Frontend: `http://localhost:3000`
- Backend API: `http://localhost:8080`

### Manual setup

See the individual project READMEs for detailed instructions:

- [Backend (Go)](back/go-assistant/README.md)
- [Frontend (React)](front/react-assistant/README.md)

## Tech stack

| Layer | Technology |
|---|---|
| Backend | Go 1.25, standard library HTTP server |
| Frontend | React 19, TypeScript 5.7, Vite 6, Tailwind CSS 4 |
| Database | PostgreSQL 16 (durable), Redis 7 (cache + HITL) |
| LLM Provider | OpenRouter (multi-model, streaming, structured output) |
| API Protocol | REST + SSE (Server-Sent Events) |
| CI/CD | GitHub Actions, GoReleaser |
| Containers | Docker Compose |

## Specifications

Detailed architecture specs live in [`.docs/specs/`](.docs/specs/):

| Document | Description |
|---|---|
| [agentic-cpn-v1.0](.docs/specs/agentic-cpn-v1.0.md) | Core CPN types, axioms, execution model |
| [agentic-cpn-v1.1](.docs/specs/agentic-cpn-v1.1.md) | Personality system, group agents |
| [agentic-cpn-v1.2](.docs/specs/agentic-cpn-v1.2.md) | HTTP API, SSE streaming, guardrails |
| [agentic-cpn-v1.3](.docs/specs/agentic-cpn-v1.3.md) | Persistence layer (PostgreSQL, Redis) |

## License

[Apache License 2.0](LICENSE)
