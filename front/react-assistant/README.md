# BRAE Frontend -- React Assistant

The frontend is a React 19 + TypeScript application with a deep-space terminal aesthetic. It provides a real-time chat interface, CPN execution monitor, personality editor, and tool catalog -- all connected to the backend via REST and Server-Sent Events.

## Tech stack

| Technology | Version | Purpose |
|---|---|---|
| React | 19 | UI framework |
| TypeScript | 5.7 | Type safety |
| Vite | 6 | Build tool + dev server |
| Tailwind CSS | 4 | Utility-first styling |
| i18next | 26 | Internationalization (ES/EN) |
| @xyflow/react | 12 | CPN topology graph visualization |
| @dagrejs/dagre | 3 | DAG layout algorithm |
| react-markdown | 10 | Markdown rendering (GFM, LaTeX, code) |
| rehype-katex | 7 | Math rendering |
| react-syntax-highlighter | 16 | Code syntax highlighting |
| Vitest | 4 | Unit/integration testing |

## Features

### Chat interface

Real-time conversation with streaming LLM responses. Messages render markdown with GitHub Flavored Markdown support, LaTeX math (inline and block via KaTeX), and syntax-highlighted code blocks with copy-to-clipboard.

- Session management (create, list, delete)
- Session forking from any point in history
- Connection status indicator with auto-reconnect
- Model selector
- Cost accumulator display

### CPN execution monitor

Live visualization of CPN execution as it happens:

- **D3 force-directed graph** showing places, transitions, and token flow in real-time
- **Timeline scrubber** for playback and rewind of execution history
- **Transition inspector** with input/output tokens, cost, and duration
- **CPN breadcrumb** for navigating sub-CPN hierarchies
- **Metrics summary** with aggregated token counts and cost

### Personality editor

Manage the agent's three-layer personality:

- **Identity card** displaying agent name, nature, creator, and pronouns
- **Principle editor** for modifying Nucleo (Id), Conducta (Ego), and Etica (Superego) layers
- **Hierarchy control** with drag-drop reordering of principle priority
- **Tension visualizer** showing friction resolution between principles
- Reset to defaults

### Additional features

- **Tool catalog** -- browse registered tools with descriptions and parameters
- **Balance widget** -- OpenRouter account balance and usage
- **Landing page** -- public-facing with waitlist signup

## Project structure

```
src/
  App.tsx                           Root component (auth + layout routing)
  main.tsx                          Vite entry point
  index.css                         Tailwind + deep-space terminal theme

  contexts/
    AuthContext.tsx                  Google OAuth context

  features/
    auth/GoogleSignIn.tsx           Google Sign-In button
    landing/LandingPage.tsx         Public landing page
    chat/                           Core chat UI
      ChatContainer.tsx             Main chat layout
      ChatHeader.tsx                Connection status, model selector, settings
      ChatSidebar.tsx               Session history, fork/delete
      MessageList.tsx               Scrollable message feed
      MessageBubble.tsx             Individual message rendering
      MessageInput.tsx              Auto-resizing textarea
      StreamingIndicator.tsx        Animated thinking dots
      MarkdownContent.tsx           Markdown + LaTeX rendering
      CodeBlock.tsx                 Syntax-highlighted code with copy
      ForkDialog.tsx                Fork session dialog
    execution-monitor/              CPN execution visualization
      ExecutionMonitor.tsx          Main panel (timeline, metrics)
      LiveGraph.tsx                 Real-time D3 force-directed graph
      TransitionInspector.tsx       Transition detail view
      TimelineScrubber.tsx          Execution playback
      CPNBreadcrumb.tsx             CPN hierarchy navigation
      MetricsSummary.tsx            Aggregated metrics
      CostAccumulator.tsx           Running cost display
    personality/                    Agent personality editor
      PersonalityPanel.tsx          Main personality UI
      PrincipleEditor.tsx           Edit individual principle
      HierarchyControl.tsx          Drag-drop principle reordering
      TensionVisualizer.tsx         Friction resolution display
      IdentityCard.tsx              Agent identity display
    cpn-visualizer/                 CPN topology graph
      TopologyGraph.tsx             @xyflow/react rendering
      EdgeRenderer.tsx              Arc rendering
    tools/ToolCatalog.tsx           Tool registry browser
    billing/BalanceWidget.tsx       Account balance display
    desktop/DesktopLayout.tsx       Root desktop layout
    i18n/LanguageSwitcher.tsx       Language toggle

  services/                         API clients (Axios)
    api.ts                          Base Axios instance
    sessionService.ts               Session CRUD, messaging
    personalityService.ts           Personality CRUD
    toolService.ts                  Tool discovery
    billingService.ts               Balance queries

  hooks/                            Custom React hooks
    useSession.ts                   Session state management
    useSse.ts                       SSE connection with auto-reconnect
    useAsync.ts                     Promise-based data fetching
    useLocalStorage.ts              Persistent client state

  types/                            TypeScript type definitions
    Session.ts, Message.ts, Event.ts, Personality.ts, ...

  i18n/                             Internationalization
    config.ts                       i18next setup (ES default, EN)
    locales/{en,es}/                Translation files (9 namespaces)
```

## Design system

The UI follows a **deep-space terminal** aesthetic -- dark backgrounds with cyan accent glows, monospace headers, and subtle scan-line overlays.

### Color palette

| Token | Value | Usage |
|---|---|---|
| `--bg-deep` | `#0a0a0f` | Near-black with blue undertone |
| `--bg-surface` | `#12121a` | Elevated surfaces |
| `--border-glow` | `#0ea5e9` | Sky-500 accent borders |
| `--accent` | `#0ea5e9` | Primary accent color |
| `--text-primary` | `#e2e8f0` | Slate-200 body text |

### Typography

- **Headers**: JetBrains Mono -- monospaced, engineered feel
- **Body**: DM Sans -- clean, geometric, highly readable

### Animations

- Blinking cyan cursor (streaming indicator)
- Bouncing dots (thinking state)
- Connection pulse (SSE status)
- Glow borders with box-shadow

## SSE integration

The `useSse` hook manages a persistent EventSource connection to the backend:

```typescript
const { isConnected, lastEvent } = useSse(sessionId, {
  onStreamChunk: (chunk) => appendToMessage(chunk),
  onEvent: (event) => updateExecutionMonitor(event),
});
```

- Auto-reconnect with exponential backoff on disconnect
- Type-safe event parsing for all CPN event types
- Automatic cleanup on component unmount

## Internationalization

Two languages supported: **Spanish** (default) and **English**.

Nine namespaces: `common`, `landing`, `auth`, `chat`, `desktop`, `monitor`, `flows`, `personality`, `tools`.

All translations are bundled synchronously at startup. Language detection uses localStorage with browser fallback.

```typescript
const { t } = useNamespace('chat');
return <button>{t('messages.send')}</button>;
```

## Setup

### Prerequisites

- Node.js 22+
- npm 10+

### Development

```bash
npm install
npm run dev
# Runs on http://localhost:5173
```

The dev server proxies `/api/*` to the backend. Set the backend URL via `VITE_API_URL` if needed.

### Production build

```bash
npm run build
# Output: dist/
```

### Testing

```bash
npm run test              # Run once
npm run test:watch        # Watch mode
npm run test:coverage     # Coverage report
```

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `VITE_API_URL` | No | Backend API URL (default: same origin) |
| `VITE_GOOGLE_CLIENT_ID` | No | Google OAuth client ID |

## Docker

The Dockerfile uses a multi-stage build: `node:22-alpine` for building, `nginx:1.27-alpine` for serving. The nginx config handles SPA routing (404 -> index.html) and reverse-proxies `/api/*` to the backend with SSE-optimized settings (buffering off, 300s timeout).
