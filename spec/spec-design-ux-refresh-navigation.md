---
title: "UX Refresh: Hybrid Navigation System — Replace Dockbar with Rail + Command Palette + Adaptive Panels"
version: 1.0
date_created: 2026-04-04
last_updated: 2026-04-04
owner: liwaisi
tags: [design, frontend, ux, navigation, react, accessibility]
---

# Introduction

This specification defines the UX refresh for the Liwaisi Assistant frontend application. The primary goal is to replace the current macOS-style bottom dockbar with a modern AI-native navigation system composed of three pillars: a **Left Navigation Rail**, a **Command Palette**, and an **Adaptive Right Panel**. Chat remains the primary surface, always accessible and visible even in dual-pane mode.

The design draws from established patterns in modern AI agent applications (Claude.ai Artifacts, ChatGPT Canvas, Cursor, Linear, Arc Browser) and aligns with 2025-2026 AI UX trends: ambient navigation, progressive disclosure, minimal chrome, and keyboard-first interaction.

## 1. Purpose & Scope

### Purpose

Replace the bottom dock (8 icons, 5 non-functional) with a navigation system that:

1. Eliminates all "Coming Soon" placeholder UI
2. Provides faster access to all active features (Chat, Flows, Monitor)
3. Enables dual-pane mode (Chat + Flows/Monitor simultaneously)
4. Introduces keyboard-first navigation via Command Palette
5. Scales gracefully as new features are added without UI clutter
6. Works across desktop, tablet, and mobile breakpoints

### Scope

- **In scope**: Navigation components, layout restructuring, responsive behavior, keyboard shortcuts, animations, accessibility
- **Out of scope**: New features (Audio, Code, Calendar, Notes), backend changes, SSE protocol changes, authentication flow changes

### Intended Audience

- Frontend developers implementing the components
- AI code generation agents building from this specification

### Assumptions

- React 19, Tailwind 4, Vite 6, TypeScript 5.7 remain the technology stack
- State-based navigation continues (no URL router)
- All existing hooks (`useChat`, `useChatList`, `useSSE`, `useExecutionMonitor`, `useBalance`) remain unchanged
- The design system (CSS custom properties, glassmorphism, dark theme, JetBrains Mono) is preserved

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Navigation Rail** | A narrow vertical bar (48px wide) on the left edge containing icon buttons for primary navigation. Follows Material Design 3 Navigation Rail pattern adapted for glassmorphism. |
| **Command Palette** | A modal overlay triggered by `Cmd+K` / `Ctrl+K` providing fuzzy-search access to all navigation targets and actions. |
| **Adaptive Panel** | A right-side panel that slides in to display Flows or Monitor content alongside the Chat view, creating a dual-pane layout. |
| **Active App** | The currently selected primary view: `chat`, `flows`, or `monitor`. Stored in component state as `ActiveApp` type. |
| **Dual-Pane Mode** | Layout state where Chat occupies the left portion and an Adaptive Panel (Flows or Monitor) occupies the right portion of the main content area. |
| **Rail Expansion** | Temporary widening of the Navigation Rail (from 48px to 280px) to reveal the Chat Sidebar session list. Only available when `activeApp === 'chat'`. |
| **CPN** | Colored Petri Net — the execution model used by the backend AI agent engine. |
| **HITL** | Human-In-The-Loop — approval/rejection interaction pattern within chat messages. |
| **SSE** | Server-Sent Events — real-time streaming protocol for messages and execution events. |
| **FAB** | Floating Action Button — a circular button floating above content for primary actions. |
| **Progressive Disclosure** | UX pattern of showing minimum viable interface by default, revealing complexity on demand. |

## 3. Requirements, Constraints & Guidelines

### Navigation Rail

- **REQ-001**: A vertical Navigation Rail SHALL be rendered on the left edge of the viewport, exactly 48px wide in its collapsed state.
- **REQ-002**: The Rail SHALL contain exactly 3 navigation icons in this order (top to bottom): Chat, Flows, Monitor. A Settings icon SHALL be positioned at the bottom of the Rail, separated from the navigation icons by a flex spacer.
- **REQ-003**: The active navigation item SHALL display an accent highlight background (`rgba(14, 165, 233, 0.12)`) with a left border accent indicator (2px solid `var(--accent)`).
- **REQ-004**: Each Rail icon SHALL show a tooltip label on hover (appearing to the right of the icon) with a 200ms delay before display.
- **REQ-005**: The Rail SHALL use the same glassmorphism styling as the current dock (`glass-surface` class) with a right border of `1px solid var(--border-dim)`.
- **REQ-006**: When `activeApp === 'chat'`, clicking the Chat Rail icon while already in chat mode SHALL toggle the Rail Expansion (expand to 280px showing the ChatSidebar session list, or collapse back to 48px).
- **REQ-007**: The Rail Expansion animation SHALL use a CSS transition of `width 200ms cubic-bezier(0.4, 0, 0.2, 1)`.
- **REQ-008**: When the Rail is expanded, the ChatSidebar content SHALL render inside the Rail below the navigation icons, reusing the existing `ChatSidebar` component's inner content (chat list, new chat button, infinite scroll).
- **REQ-009**: The Settings Rail icon SHALL open a floating settings panel (slide-over from left, 320px wide) with placeholder content: "Settings — Coming Soon" in muted text. It SHALL NOT show a "Coming Soon" tooltip — the panel itself communicates the status.

### Command Palette

- **REQ-010**: A Command Palette SHALL be accessible globally via `Cmd+K` (macOS) / `Ctrl+K` (Windows/Linux) keyboard shortcut.
- **REQ-011**: The Command Palette SHALL render as a centered modal overlay with glassmorphism background, `backdrop-filter: blur(20px)`, and a semi-transparent backdrop (`rgba(0, 0, 0, 0.5)`).
- **REQ-012**: The Palette SHALL contain a text input at the top with placeholder text "Type a command or search..." styled with `var(--bg-input)` background and `var(--border-glow)` focus border.
- **REQ-013**: The Palette SHALL support the following action categories and items:

  | Category | Action | Shortcut | Handler |
  |----------|--------|----------|---------|
  | Navigation | Go to Chat | `Cmd+1` | `setActiveApp('chat')` |
  | Navigation | Go to Flows | `Cmd+2` | `setActiveApp('flows')` |
  | Navigation | Go to Monitor | `Cmd+3` | `setActiveApp('monitor')` |
  | Chat | New Chat | `Cmd+N` | `chatList.createChat()` |
  | Chat | Search Chats | — | Filter chat list by title |
  | View | Toggle Chat Sidebar | `Cmd+B` | Toggle Rail Expansion |
  | View | Open Flows Panel | `Cmd+Shift+F` | Open Flows in Adaptive Panel |
  | View | Open Monitor Panel | `Cmd+Shift+M` | Open Monitor in Adaptive Panel |
  | View | Close Panel | `Escape` | Close Adaptive Panel |

- **REQ-014**: The Palette SHALL implement fuzzy matching on action labels and category names, ranking results by relevance score.
- **REQ-015**: The Palette SHALL support keyboard navigation: `ArrowUp`/`ArrowDown` to move selection, `Enter` to execute, `Escape` to close.
- **REQ-016**: The Palette SHALL close automatically after executing an action.
- **REQ-017**: When in "Search Chats" mode (triggered by typing after selecting the search category or by starting input with `>`), the Palette SHALL display matching chat sessions from `chatList.chats` filtered by title, with each result showing the chat title, preview, and time ago.
- **REQ-018**: The Palette modal SHALL be rendered via a React Portal attached to `document.body` to avoid z-index stacking issues.

### Adaptive Right Panel

- **REQ-019**: An Adaptive Right Panel system SHALL support displaying Flows or Monitor content alongside the Chat view in a dual-pane layout.
- **REQ-020**: The Panel SHALL slide in from the right edge with a `transform: translateX` transition of `250ms cubic-bezier(0.4, 0, 0.2, 1)`.
- **REQ-021**: The Panel default width SHALL be 50% of the remaining content area (after the Rail), with a minimum width of 400px and a maximum width of 70%.
- **REQ-022**: The Panel SHALL include a resize handle (6px wide, centered vertically) on its left edge that supports mouse drag to resize. The handle SHALL show `var(--border-dim)` by default and `var(--accent)` on hover.
- **REQ-023**: If the Panel is resized below its minimum width (400px), it SHALL snap-to-collapse (close completely) with a smooth transition.
- **REQ-024**: The Panel SHALL include a header bar with: the panel title ("Flows" or "Monitor"), a close button (X icon), and a "Pop Out" button that switches to full-content mode (hides Chat, Panel takes full width).
- **REQ-025**: The Panel content SHALL render the same `FlowBrowser`, `FlowDetail`, or `ExecutionMonitor` components currently used in full-screen mode, without modification to those components.
- **REQ-026**: When the Adaptive Panel is open, the Chat area SHALL remain fully functional (message input, streaming, HITL actions, scrolling).

### Layout Orchestration

- **REQ-027**: The `DesktopLayout` component SHALL manage layout state with the following state variables:
  ```typescript
  type ActiveApp = 'chat' | 'flows' | 'monitor';
  type PanelContent = 'flows' | 'monitor' | null;
  type LayoutMode = 'single' | 'dual';

  const [activeApp, setActiveApp] = useState<ActiveApp>('chat');
  const [panelContent, setPanelContent] = useState<PanelContent>(null);
  const [isRailExpanded, setIsRailExpanded] = useState(false);
  const [isPaletteOpen, setIsPaletteOpen] = useState(false);
  ```
- **REQ-028**: The layout SHALL follow this structure (top to bottom, left to right):
  ```
  ┌─────────────────────────────────────────────────┐
  │                   StatusBar                      │
  ├──────┬──────────────────────┬────────────────────┤
  │      │                      │                    │
  │ Rail │    Main Content      │  Adaptive Panel    │
  │ 48px │    (Chat/Flows/      │  (optional)        │
  │      │     Monitor)         │                    │
  │      │                      │                    │
  ├──────┴──────────────────────┴────────────────────┤
  ```
- **REQ-029**: When `activeApp` is `'flows'` or `'monitor'` (selected via Rail), the content SHALL render full-width (no Adaptive Panel). The Panel is only available when `activeApp === 'chat'`.
- **REQ-030**: Clicking a Rail icon for a different mode than `activeApp` SHALL switch the full view. Clicking the same Rail icon when already active SHALL have no effect (except Chat, which toggles sidebar per REQ-006).
- **REQ-031**: The `pb-20` padding on the MessageInput area (currently compensating for the dock) SHALL be removed.

### Keyboard Shortcuts

- **REQ-032**: The following global keyboard shortcuts SHALL be registered via a `useEffect` on the `DesktopLayout` component:

  | Shortcut | Action | Condition |
  |----------|--------|-----------|
  | `Cmd/Ctrl + K` | Toggle Command Palette | Always |
  | `Cmd/Ctrl + 1` | Switch to Chat | Always |
  | `Cmd/Ctrl + 2` | Switch to Flows | Always |
  | `Cmd/Ctrl + 3` | Switch to Monitor | Always |
  | `Cmd/Ctrl + N` | Create new chat | Always |
  | `Cmd/Ctrl + B` | Toggle Rail Expansion | `activeApp === 'chat'` |
  | `Cmd/Ctrl + Shift + F` | Toggle Flows Panel | `activeApp === 'chat'` |
  | `Cmd/Ctrl + Shift + M` | Toggle Monitor Panel | `activeApp === 'chat'` |
  | `Escape` | Close Panel / Close Palette | Panel open or Palette open |

- **REQ-033**: Keyboard shortcuts SHALL NOT fire when the user is focused on a text input, textarea, or contenteditable element, EXCEPT for `Cmd/Ctrl + K` which SHALL always work.
- **REQ-034**: Keyboard shortcuts SHALL call `e.preventDefault()` to avoid browser default actions (e.g., `Cmd+N` opening a new browser window, `Cmd+K` opening browser search bar).

### Responsive Design

- **REQ-035**: Desktop layout (viewport width >= 1024px): Left Rail (48px) + Main Content + Optional Adaptive Panel.
- **REQ-036**: Tablet layout (viewport width 640px-1023px): Left Rail (48px, no expansion) + Main Content. Adaptive Panel is disabled; Flows and Monitor render full-screen only.
- **REQ-037**: Mobile layout (viewport width < 640px): The Rail SHALL be hidden. A bottom tab bar (height 56px) with 3 icons (Chat, Flows, Monitor) SHALL be rendered instead. The tab bar SHALL use the same glassmorphism styling. No Command Palette shortcut (Cmd+K) on mobile; a search icon in the StatusBar provides equivalent access.
- **REQ-038**: Responsive breakpoints SHALL be implemented using Tailwind 4 responsive prefixes (`sm:`, `md:`, `lg:`) and a `useMediaQuery` custom hook for JavaScript-dependent behavior (e.g., disabling Panel on tablet).

### Animation & Transitions

- **REQ-039**: All navigation transitions SHALL complete within 100-300ms.
- **REQ-040**: The following animations SHALL be implemented:

  | Animation | Duration | Easing | Property |
  |-----------|----------|--------|----------|
  | Rail icon hover | 150ms | `ease-out` | `background-color`, `color` |
  | Rail expansion | 200ms | `cubic-bezier(0.4, 0, 0.2, 1)` | `width` |
  | Panel slide-in | 250ms | `cubic-bezier(0.4, 0, 0.2, 1)` | `transform` |
  | Panel slide-out | 200ms | `cubic-bezier(0.4, 0, 0.2, 1)` | `transform` |
  | Palette appear | 150ms | `ease-out` | `opacity`, `transform` (scale 0.98 -> 1) |
  | Palette dismiss | 100ms | `ease-in` | `opacity` |
  | View switch | 150ms | `ease-out` | `opacity` (crossfade) |

- **REQ-041**: Animations SHALL respect the `prefers-reduced-motion` media query. When reduced motion is preferred, all transitions SHALL be instant (0ms duration).

### Accessibility

- **ACC-001**: The Navigation Rail SHALL have `role="navigation"` and `aria-label="Main navigation"`.
- **ACC-002**: Each Rail icon button SHALL have `aria-label` matching its label text and `aria-current="page"` when active.
- **ACC-003**: The Command Palette SHALL have `role="dialog"`, `aria-modal="true"`, and `aria-label="Command palette"`. The results list SHALL have `role="listbox"` with `aria-activedescendant` tracking the selected item.
- **ACC-004**: The Adaptive Panel SHALL have `role="complementary"` and `aria-label` matching its content ("Flows panel" or "Monitor panel").
- **ACC-005**: Focus SHALL be trapped within the Command Palette when open.
- **ACC-006**: When the Command Palette closes, focus SHALL return to the previously focused element.
- **ACC-007**: The bottom tab bar (mobile) SHALL have `role="tablist"` with each tab having `role="tab"` and `aria-selected`.
- **ACC-008**: All interactive elements SHALL be reachable via Tab key navigation in a logical order: Rail icons (top to bottom) -> Main content -> Panel content.
- **ACC-009**: Color contrast ratios SHALL meet WCAG 2.1 AA standards (minimum 4.5:1 for text, 3:1 for UI components).

### Design System

- **DSN-001**: New CSS custom properties SHALL be added to `:root` in `index.css`:
  ```css
  --rail-width: 48px;
  --rail-expanded-width: 280px;
  --panel-min-width: 400px;
  --panel-default-ratio: 0.5;
  --transition-fast: 150ms;
  --transition-normal: 250ms;
  --transition-easing: cubic-bezier(0.4, 0, 0.2, 1);
  ```
- **DSN-002**: The `dock-icon` CSS class and related hover/active animations SHALL be removed from `index.css`.
- **DSN-003**: New CSS classes SHALL be added:
  - `.nav-rail` — Rail container styling
  - `.nav-rail-item` — Rail button styling with hover/active states
  - `.nav-rail-item--active` — Active state indicator
  - `.command-palette-backdrop` — Palette overlay backdrop
  - `.command-palette-modal` — Palette modal container
  - `.adaptive-panel` — Panel container with slide transition
  - `.panel-resize-handle` — Drag handle styling
  - `.mobile-tab-bar` — Bottom tab bar for mobile
  - `.mobile-tab-item` — Mobile tab button with active states

### Security

- **SEC-001**: The Command Palette SHALL NOT execute arbitrary user input as code. All actions are predefined in an action registry.
- **SEC-002**: Chat search within the Palette SHALL sanitize search queries before filtering (no regex injection from user input).
- **SEC-003**: Keyboard shortcut handlers SHALL NOT expose internal state or component references to the DOM.

### Constraints

- **CON-001**: No URL routing library SHALL be introduced. Navigation remains state-based.
- **CON-002**: No new npm dependencies SHALL be added for the Navigation Rail or Adaptive Panel. The Command Palette MAY use `cmdk` (by Paco Coursey) as an optional dependency, OR be implemented from scratch using native React.
- **CON-003**: The existing hook interfaces (`useChat`, `useChatList`, `useSSE`, `useExecutionMonitor`, `useBalance`) SHALL NOT be modified.
- **CON-004**: The `FlowBrowser`, `FlowDetail`, `ExecutionMonitor`, `MessageList`, `MessageInput`, `MessageBubble`, `ForkDialog`, `ConfirmClearDialog`, `MarkdownContent`, and `CodeBlock` components SHALL NOT be modified.
- **CON-005**: All CSS custom properties defined in the current `index.css` `:root` block SHALL be preserved.
- **CON-006**: The `scan-lines` overlay effect SHALL be preserved.
- **CON-007**: Total JavaScript bundle size increase SHALL NOT exceed 15KB gzipped.

### Guidelines

- **GUD-001**: Prefer CSS transitions over JavaScript animation libraries for navigation animations.
- **GUD-002**: Use `will-change: transform` on elements that animate `transform` properties for GPU-accelerated compositing.
- **GUD-003**: Use semantic HTML elements: `<nav>` for Rail, `<aside>` for Panel, `<dialog>` for Palette modal.
- **GUD-004**: Follow the existing naming convention: PascalCase for components, camelCase for hooks, kebab-case for CSS classes.
- **GUD-005**: Keep the Rail icon SVGs consistent with the existing icon set (16x16 viewBox 0 0 24 24, stroke-based, 1.5px stroke width).
- **GUD-006**: All new components SHALL be function components using React hooks.

### Patterns

- **PAT-001**: State lifting pattern — `DesktopLayout` owns all layout state (`activeApp`, `panelContent`, `isRailExpanded`, `isPaletteOpen`) and passes handlers down via props.
- **PAT-002**: Action registry pattern — Command Palette actions are defined as a static array of `{ id, label, category, shortcut?, handler, keywords? }` objects, enabling fuzzy search and keyboard shortcut registration from a single source of truth.
- **PAT-003**: Render slot pattern — The Adaptive Panel accepts `children` as its content, receiving the appropriate component (`FlowBrowser`, `FlowDetail`, or `ExecutionMonitor`) from the parent layout.

## 4. Interfaces & Data Contracts

### Component Props Interfaces

```typescript
// NavigationRail.tsx
interface NavigationRailProps {
  activeApp: ActiveApp;
  isExpanded: boolean;
  onNavigate: (app: ActiveApp) => void;
  onToggleExpansion: () => void;
  onSettingsClick: () => void;
  /** ChatSidebar content rendered inside expanded rail */
  sidebarContent?: React.ReactNode;
}

// CommandPalette.tsx
interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  actions: PaletteAction[];
}

interface PaletteAction {
  id: string;
  label: string;
  category: 'Navigation' | 'Chat' | 'View';
  shortcut?: string;
  icon?: React.ReactNode;
  keywords?: string[];
  handler: () => void;
}

// AdaptivePanel.tsx
interface AdaptivePanelProps {
  isOpen: boolean;
  title: string;
  onClose: () => void;
  onPopOut: () => void;
  children: React.ReactNode;
}

// MobileTabBar.tsx
interface MobileTabBarProps {
  activeApp: ActiveApp;
  onNavigate: (app: ActiveApp) => void;
}
```

### Layout State Machine

```
States:
  - CHAT_ONLY: activeApp='chat', panelContent=null, isRailExpanded=false
  - CHAT_WITH_SIDEBAR: activeApp='chat', panelContent=null, isRailExpanded=true
  - CHAT_WITH_PANEL: activeApp='chat', panelContent='flows'|'monitor', isRailExpanded=false
  - CHAT_WITH_SIDEBAR_AND_PANEL: activeApp='chat', panelContent='flows'|'monitor', isRailExpanded=true
  - FLOWS_FULL: activeApp='flows', panelContent=null, isRailExpanded=false
  - MONITOR_FULL: activeApp='monitor', panelContent=null, isRailExpanded=false

Transitions:
  Rail click (Chat):
    from CHAT_ONLY -> CHAT_WITH_SIDEBAR
    from CHAT_WITH_SIDEBAR -> CHAT_ONLY
    from FLOWS_FULL -> CHAT_ONLY
    from MONITOR_FULL -> CHAT_ONLY

  Rail click (Flows):
    from any -> FLOWS_FULL (closes panel and sidebar)

  Rail click (Monitor):
    from any -> MONITOR_FULL (closes panel and sidebar)

  Cmd+Shift+F (toggle Flows panel):
    from CHAT_ONLY -> CHAT_WITH_PANEL(flows)
    from CHAT_WITH_PANEL(flows) -> CHAT_ONLY
    from CHAT_WITH_PANEL(monitor) -> CHAT_WITH_PANEL(flows)

  Cmd+Shift+M (toggle Monitor panel):
    from CHAT_ONLY -> CHAT_WITH_PANEL(monitor)
    from CHAT_WITH_PANEL(monitor) -> CHAT_ONLY
    from CHAT_WITH_PANEL(flows) -> CHAT_WITH_PANEL(monitor)

  Panel pop-out:
    from CHAT_WITH_PANEL(flows) -> FLOWS_FULL
    from CHAT_WITH_PANEL(monitor) -> MONITOR_FULL

  Panel close / Escape:
    from CHAT_WITH_PANEL(*) -> CHAT_ONLY
```

### File Structure (new and modified)

```
src/features/desktop/
  DesktopLayout.tsx        [MODIFY] — New layout with Rail + Panel orchestration
  StatusBar.tsx            [MODIFY] — Add mode breadcrumb, keyboard hint
  NavigationRail.tsx       [CREATE] — Left navigation rail component
  CommandPalette.tsx       [CREATE] — Command palette modal
  AdaptivePanel.tsx        [CREATE] — Right-side adaptive panel with resize
  MobileTabBar.tsx         [CREATE] — Bottom tab bar for mobile
  Dock.tsx                 [DELETE] — Replaced by NavigationRail
  DockIcon.tsx             [DELETE] — Replaced by Rail items

src/features/chat/
  ChatSidebar.tsx          [MODIFY] — Extract inner content for reuse in Rail expansion

src/hooks/
  useMediaQuery.ts         [CREATE] — Responsive breakpoint hook
  useKeyboardShortcuts.ts  [CREATE] — Global shortcut registration hook

src/index.css              [MODIFY] — Remove dock styles, add rail/panel/palette styles
```

## 5. Acceptance Criteria

- **AC-001**: Given the application is loaded on desktop, When the user sees the left edge of the viewport, Then a 48px Navigation Rail with Chat, Flows, Monitor icons and a bottom Settings icon SHALL be visible.
- **AC-002**: Given the user is on the Chat view, When the user clicks the Chat icon in the Rail, Then the Rail SHALL expand to 280px showing the chat session list sidebar.
- **AC-003**: Given the Rail is expanded, When the user clicks the Chat icon again OR clicks outside the sidebar area, Then the Rail SHALL collapse to 48px.
- **AC-004**: Given the user presses `Cmd+K`, Then the Command Palette SHALL appear centered on screen with an empty search input focused.
- **AC-005**: Given the Command Palette is open, When the user types "new", Then "New Chat" SHALL appear as a result. When the user presses Enter, a new chat SHALL be created and the Palette SHALL close.
- **AC-006**: Given the user is on the Chat view, When the user presses `Cmd+Shift+F`, Then the Flows panel SHALL slide in from the right occupying 50% of the content area, with Chat still visible and functional on the left.
- **AC-007**: Given the Flows panel is open, When the user drags the resize handle to the left beyond 400px minimum, Then the panel SHALL snap-close with a smooth animation.
- **AC-008**: Given the user clicks the Flows icon in the Rail, Then the full-screen Flows view SHALL render (no dual-pane), and any open panel SHALL close.
- **AC-009**: Given the viewport is less than 640px wide, Then the Navigation Rail SHALL be hidden and a bottom tab bar with 3 icons SHALL be visible.
- **AC-010**: Given the application is loaded, Then there SHALL be zero instances of "Coming Soon" text visible anywhere in the UI.
- **AC-011**: Given the user is in dual-pane mode (Chat + Monitor), When the user receives a streaming message via SSE, Then the message SHALL stream correctly in the Chat pane while the Monitor displays execution data.
- **AC-012**: Given the user presses `Cmd+1`, `Cmd+2`, or `Cmd+3`, Then the active app SHALL switch to Chat, Flows, or Monitor respectively.
- **AC-013**: Given the user has `prefers-reduced-motion: reduce` enabled, Then all transition durations SHALL be 0ms.
- **AC-014**: Given a screen reader is active, Then all navigation elements SHALL announce their labels and states correctly.
- **AC-015**: Given the user is typing in the MessageInput, When they press `Cmd+2`, Then the shortcut SHALL be captured (prevented from typing "2") and the view SHALL switch to Flows.

## 6. Test Automation Strategy

### Test Levels

- **Unit Tests**: Individual component rendering and state logic (NavigationRail, CommandPalette, AdaptivePanel, MobileTabBar)
- **Integration Tests**: Layout orchestration — state transitions between views, dual-pane behavior, Rail expansion with ChatSidebar
- **End-to-End Tests**: Full user journeys — navigation flow, keyboard shortcuts, responsive behavior

### Frameworks

- **Vitest** for unit and integration tests (already configured via Vite)
- **React Testing Library** for component tests
- **Playwright** for E2E browser tests (desktop + mobile viewports)

### Test Cases

| ID | Level | Description |
|----|-------|-------------|
| T-001 | Unit | NavigationRail renders 3 nav icons + 1 settings icon |
| T-002 | Unit | NavigationRail highlights active app correctly |
| T-003 | Unit | CommandPalette fuzzy-matches actions by label and keywords |
| T-004 | Unit | CommandPalette keyboard navigation (up/down/enter/escape) |
| T-005 | Unit | AdaptivePanel respects min/max width constraints |
| T-006 | Unit | MobileTabBar renders on narrow viewport, hidden on wide |
| T-007 | Integration | Clicking Rail icon switches activeApp state |
| T-008 | Integration | Cmd+K opens palette, Escape closes it |
| T-009 | Integration | Dual-pane mode: Chat + Flows panel render simultaneously |
| T-010 | Integration | Rail expansion shows ChatSidebar content |
| T-011 | Integration | Panel pop-out transitions to full-screen mode |
| T-012 | E2E | Full navigation journey: Chat -> Cmd+Shift+F -> resize panel -> pop out -> Cmd+1 back to chat |
| T-013 | E2E | Mobile viewport: bottom tabs visible, rail hidden, full-screen views |
| T-014 | E2E | Keyboard shortcuts work from all views |
| T-015 | Accessibility | axe-core audit on all layout states passes with 0 violations |

### Coverage Requirements

- Minimum 80% line coverage for new components
- 100% coverage for keyboard shortcut handler logic
- 100% coverage for layout state machine transitions

## 7. Rationale & Context

### Why Replace the Dockbar

1. **5 of 8 dock icons are non-functional** — "Coming Soon" tooltips create a broken-product impression. The dock promises 8 capabilities but delivers 3.
2. **macOS dock metaphor doesn't fit a web AI assistant** — Desktop OS docks solve app-switching between unrelated applications. Within a single web app, navigation should be contextual and hierarchical.
3. **Bottom positioning wastes vertical space** — The dock occupies ~56px of vertical space plus 20px padding (`pb-20` on MessageInput), reducing the chat area. A left rail uses underutilized horizontal space.
4. **No keyboard navigation** — The current dock is mouse-only. AI agent power users expect keyboard-first workflows.
5. **No dual-pane capability** — Users currently cannot view chat and execution monitor simultaneously, despite this being a common workflow during CPN debugging.

### Why This Specific Approach

- **Left Rail**: Proven pattern (VS Code, Slack, Discord, Material Design 3). Scales to unlimited items without horizontal space pressure. Compatible with the existing dark glassmorphism aesthetic.
- **Command Palette**: Standard in developer tools (VS Code, Linear, Notion). Infinitely scalable — adding 50 new actions doesn't change the UI. The target audience (AI/CPN engineers) expects this pattern.
- **Adaptive Panel**: Follows the Claude.ai Artifacts / ChatGPT Canvas pattern that is now the dominant AI application layout. Chat-as-primary-surface matches the product's identity as a conversational AI assistant.
- **No new routing library**: The app is a single-view application with a small number of modes. Client-side state management is sufficient and avoids unnecessary complexity.

### Research Sources

This design is informed by analysis of: Claude.ai (Anthropic), ChatGPT/Canvas (OpenAI), Cursor 3 (Anysphere), Linear, Arc Browser, Notion, VS Code, Figma, Material Design 3 Navigation Rail, and 2025-2026 AI UX trend analyses from Nielsen Norman Group, UX Magazine, and CopilotKit.

## 8. Dependencies & External Integrations

### Technology Platform Dependencies

- **PLT-001**: React 19 — Required for concurrent rendering and hooks API.
- **PLT-002**: Tailwind CSS 4 — Utility-first CSS framework with responsive prefixes.
- **PLT-003**: TypeScript 5.7 — Type safety for all new components and interfaces.
- **PLT-004**: Vite 6 — Build tool and dev server.

### Optional Third-Party Dependencies

- **SVC-001**: `cmdk` (npm package by Paco Coursey) — Command palette component library. OPTIONAL: may be implemented from scratch if bundle size is a concern. If used, estimated impact: ~4KB gzipped. Required capabilities: fuzzy search, keyboard navigation, composable groups, accessible by default.

### Internal Dependencies

- **INT-001**: `useChat` hook — Provides message state, session state, send/resolve functions.
- **INT-002**: `useChatList` hook — Provides session list, CRUD operations, fork capability.
- **INT-003**: `useExecutionMonitor` hook — Provides CPN execution state and handlers.
- **INT-004**: `useSSE` hook (via `useChat`) — Real-time event streaming.
- **INT-005**: `useBalance` hook (via `BalanceWidget`) — Balance display data.
- **INT-006**: `AuthContext` — User authentication state and logout.

### Preserved Components (No Modification)

- `FlowBrowser`, `FlowDetail`, `TopologyGraph` — CPN visualizer
- `ExecutionMonitor` and sub-components — Execution monitor
- `MessageList`, `MessageInput`, `MessageBubble` — Chat messages
- `ForkDialog`, `ConfirmClearDialog` — Modal dialogs
- `MarkdownContent`, `CodeBlock` — Content rendering
- `BalanceWidget` — Billing display
- `LoginScreen` — Authentication flow

## 9. Examples & Edge Cases

### Example: Rail Expansion with Simultaneous Panel

```
┌──────────────────────────────────────────────────────┐
│                     StatusBar                         │
├─────────────┬───────────────────┬────────────────────┤
│  Rail       │                   │                    │
│  (expanded) │   Chat Messages   │   Flows Panel      │
│  280px      │   & Input         │   (resizable)      │
│             │                   │                    │
│  [Sessions] │                   │                    │
│  - Chat 1   │                   │                    │
│  - Chat 2 * │                   │                    │
│  - Chat 3   │                   │                    │
│             │                   │                    │
│  [Settings] │                   │                    │
├─────────────┴───────────────────┴────────────────────┤
```

### Example: Mobile Layout

```
┌──────────────────────┐
│      StatusBar       │
├──────────────────────┤
│                      │
│   Full-Screen View   │
│   (Chat / Flows /    │
│    Monitor)          │
│                      │
│                      │
├──────────────────────┤
│  [Chat] [Flows] [Mon]│
│     Bottom Tab Bar   │
└──────────────────────┘
```

### Edge Case: Rapid View Switching

When the user rapidly presses `Cmd+1`, `Cmd+2`, `Cmd+3` in quick succession:
- Each state change SHALL cancel any in-progress transition animation
- Only the final `activeApp` value SHALL determine the rendered view
- No intermediate rendering artifacts SHALL be visible
- React's batched state updates handle this naturally

### Edge Case: Panel Open Then Rail Click to Different Mode

When the user has Chat + Flows Panel open and clicks Monitor in the Rail:
- The Panel SHALL close (slide out)
- The view SHALL switch to full-screen Monitor
- Both transitions MAY happen simultaneously (panel slides out while content crossfades)
- `panelContent` is set to `null` and `activeApp` to `'monitor'` in a single state update

### Edge Case: Resize Panel to Collapse While Content is Loading

When the user drags the panel below minimum width while a Flow graph is loading:
- The panel SHALL snap-collapse regardless of loading state
- The flow request SHALL NOT be cancelled (it may be needed if the user reopens the panel)
- If the panel is reopened, it SHALL show the current state of the content (loaded or still loading)

### Edge Case: Command Palette While Streaming

When the user opens the Command Palette while a message is streaming:
- The streaming SHALL continue in the background
- The Palette overlay SHALL not block SSE events
- Executing a "Go to Flows" action while streaming SHALL switch views but not interrupt the stream

### Edge Case: Browser Default Shortcut Conflicts

- `Cmd+N` (New Chat) conflicts with "New Browser Window" — `preventDefault()` required
- `Cmd+K` conflicts with browser search bar in some browsers — `preventDefault()` required
- `Cmd+B` conflicts with "Toggle Bookmarks Bar" in Chrome — `preventDefault()` required
- `Cmd+1/2/3` conflicts with "Switch Tab" in browsers — `preventDefault()` required on the specific combo

**Mitigation**: All shortcuts call `e.preventDefault()` immediately. If a user needs browser defaults, they can use the Command Palette instead (no conflicting shortcut for that workflow).

## 10. Validation Criteria

1. **Visual Regression**: Side-by-side screenshots of all 6 layout states match the design specification.
2. **Interaction Audit**: All 9 keyboard shortcuts trigger the correct action from every layout state.
3. **Responsive Audit**: Layout renders correctly at 375px (mobile), 768px (tablet), 1280px (desktop), and 1920px (large desktop) viewports.
4. **Accessibility Audit**: Lighthouse accessibility score >= 90. axe-core scan returns 0 critical/serious violations.
5. **Performance Audit**: No jank during transitions (all animations maintain 60fps). Measured via Chrome DevTools Performance panel.
6. **Bundle Size**: `npm run build` shows less than 15KB gzipped increase compared to current build.
7. **Functional Regression**: All existing features work — SSE streaming, HITL approval/rejection, message forking, flow visualization, execution monitoring, session management, authentication.
8. **State Machine Completeness**: Every transition in the state machine (Section 4) is tested and produces the correct layout.

## 11. Related Specifications / Further Reading

- [spec-design-cpn-execution-monitor.md](/spec/spec-design-cpn-execution-monitor.md) — Execution Monitor component (rendered inside Adaptive Panel)
- [spec-design-cpn-execution-visualizer.md](/spec/spec-design-cpn-execution-visualizer.md) — CPN Visualizer (FlowBrowser/FlowDetail rendered inside Adaptive Panel)
- [spec-design-multi-chat-conversation-forking.md](/spec/spec-design-multi-chat-conversation-forking.md) — Fork Dialog and session management
- [spec-architecture-block20-llm-streaming.md](/spec/spec-architecture-block20-llm-streaming.md) — SSE streaming architecture
- [Material Design 3 — Navigation Rail](https://m3.material.io/components/navigation-rail/overview) — Rail design reference
- [cmdk by Paco Coursey](https://cmdk.paco.me/) — Command palette library reference
- [Claude.ai Artifacts pattern](https://claude.ai/) — Dual-pane chat+artifact reference
