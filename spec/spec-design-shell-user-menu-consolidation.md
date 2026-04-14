---
title: "Shell Consolidation: Unified User Menu, Slim Header, and Rail-Anchored Account Surface"
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: liwaisi
tags: [design, frontend, ux, navigation, react, accessibility, a11y]
---

# Introduction

This specification defines a shell-level redesign of the `front/react-assistant` application that consolidates **all configuration-related entry points** into a single avatar-anchored user menu located at the bottom of the left Navigation Rail. The goal is to eliminate the current duplication between the header ("Configuración", "Cerrar sesión") and the rail ("Identidad" gear), slim the top `StatusBar`, and align the product with 2026 SaaS shell patterns established by Linear, Vercel (Feb 2026 dashboard), and Raycast.

This spec supersedes the user-facing header affordances defined in [`spec-design-ux-refresh-navigation.md`](./spec-design-ux-refresh-navigation.md) related to the account area and `Configuración` button. All other rail, command palette, and adaptive panel requirements from that spec remain in force unless explicitly overridden below.

## 1. Purpose & Scope

### Purpose

Deliver a shell in which:

1. A user has **exactly one** place to change anything about their account, workspace, preferences, or session (the new `UserMenu`).
2. The top header (`StatusBar`) is reduced to ambient, read-oriented status (brand, app label, session/connection indicators, `⌘K` hint, balance, language).
3. Primary navigation (`Chat`, `Flujos`, `Monitor`) and agent-scope configuration (`Herramientas`, `Identidad`) remain in the rail.
4. Power users can reach every configuration action from the Command Palette (`⌘K`).
5. The rail's collapsed/expanded state is remembered across sessions.

### In Scope

- `StatusBar.tsx` header slim-down.
- `NavigationRail.tsx` bottom avatar button + local-storage persistence of expansion state.
- A new `UserMenu.tsx` popover component.
- `CommandPalette.tsx` extension with account commands.
- `DesktopLayout.tsx` prop wiring.
- i18n key additions in `src/i18n/locales/{es,en}/desktop.json`.
- Accessibility (ARIA, keyboard nav, focus management).

### Out of Scope

- Real multi-workspace switching (the workspace row is a visual placeholder; clicking "Switch workspace" opens a disabled/stub confirmation).
- Changes to `SettingsPage.tsx` contents.
- Changes to `AuthContext.tsx` semantics.
- Mobile tab bar (`MobileTabBar`) — only the desktop shell is affected, though `UserMenu` MUST not break mobile layout.
- Billing UI (the "Ir a facturación" command is a route stub; no new billing page is built here).

### Intended Audience

- Frontend engineers implementing the components.
- AI code-generation agents acting from this spec.
- Reviewers validating acceptance criteria.

### Assumptions

- React 19, Tailwind v4, Vite, TypeScript remain the stack.
- State-based (non-URL) navigation continues via `useSessionManager`.
- Existing design tokens (`--bg-deep`, `--bg-surface`, `--accent`, `--border-dim`, JetBrains Mono) are preserved.
- `AuthContext` exposes `user` (with `name`, `email`, `picture`, `is_admin`) and `logout()`.
- `LanguageSwitcher.tsx` is the canonical dropdown primitive to model `UserMenu` on.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Shell** | The persistent UI chrome: `StatusBar` (header) + `NavigationRail` (left) + `CommandPalette` (⌘K) + `AdaptivePanel` (right, chat-only). |
| **UserMenu** | New popover component anchored to the avatar button at the bottom of the Navigation Rail; holds workspace, preferences, admin entry, and logout. |
| **Avatar Anchor** | The circular button in the rail bottom showing `user.picture` (fallback: first-letter monogram) that toggles `UserMenu`. |
| **Popover** | A floating surface positioned next to its trigger, constrained to viewport, with its own focus scope. Not modal. Dismisses on outside click, `Esc`, focus loss, or route change. |
| **Rail Expansion** | The 48 px ↔ 260 px width toggle of the Navigation Rail. Persisted to `localStorage` under the key `liwaisi_rail_expanded`. |
| **Account Commands** | New ⌘K commands grouped under "Cuenta": *Abrir ajustes*, *Cambiar idioma*, *Cerrar sesión*, *Ir a facturación*, *Cambiar workspace*. |
| **Glass Surface** | Existing CSS class `.glass-surface` providing `backdrop-filter: blur(16px)` over `rgba(18, 18, 26, 0.82)`. |
| **Focus Trap** | Keyboard focus is constrained inside a component while it is open; `Tab` / `Shift+Tab` cycle within the component only. |

## 3. Requirements, Constraints & Guidelines

### 3.1 StatusBar (Header)

- **REQ-001**: `StatusBar.tsx` MUST NOT render the user avatar image, the user name, the "Configuración" button, or the "Cerrar sesión" button.
- **REQ-002**: `StatusBar.tsx` MUST retain, in this order, from left to right: brand badge (`✦ brae OS`), app-label chip (`CHAT` / `FLUJOS` / `MONITOR`), session-state dot + label, connection-state dot, personality principle badges (desktop only, `md+`), `⌘K` hint pill, `BalanceWidget`, `LanguageSwitcher`.
- **REQ-003**: `StatusBar` MUST accept an unchanged props surface except that `onOpenSettings` is removed. Parent (`DesktopLayout`) MUST stop passing it.
- **REQ-004**: The header MUST retain its `glass-surface` background, `var(--border-dim)` bottom border, and 48 px total height.
- **CON-001**: `StatusBar` MUST NOT import `useAuth`. It MUST NOT know about `user` or `logout`.

### 3.2 NavigationRail

- **REQ-010**: `NavigationRail.tsx` MUST render a user Avatar Anchor button as the last item of the rail's bottom cluster, below `Herramientas`, `Admin` (when visible), and `Identidad`.
- **REQ-011**: The Avatar Anchor button MUST display `user.picture` as an `<img>` with `alt=""` and the component's accessible label provided via `aria-label="{user.name} — abrir menú de cuenta"` (i18n-keyed).
- **REQ-012**: If `user.picture` fails to load or is empty, the Avatar Anchor MUST fall back to a monogram (`user.name` first grapheme, uppercased) over a solid `var(--bg-surface)` background with `var(--accent)` text.
- **REQ-013**: The Avatar Anchor MUST expose `aria-haspopup="menu"`, `aria-expanded` reflecting open state, and `aria-controls="user-menu-popover"`.
- **REQ-014**: Clicking the Avatar Anchor MUST open the `UserMenu` popover anchored to the right side of the avatar (the menu "pops out" toward the main content area, never off-screen). On narrow viewports (`width < 480 px`) it MUST open as a bottom-sheet variant occupying the lower half of the viewport.
- **REQ-015**: The `NavigationRail` MUST persist its `isExpanded` state to `localStorage` under the key `liwaisi_rail_expanded` using the string values `"1"` / `"0"`. On mount, it MUST hydrate from this key (default: `"0"` collapsed) **before first paint** to avoid layout flash.
- **REQ-016**: The existing bottom gear icon (`onSettingsClick` → `activeApp='personality'`) keeps its semantic meaning ("Identidad") and MUST remain. It does NOT open settings; the `UserMenu` is the settings surface.
- **REQ-017**: The Avatar Anchor MUST NOT be rendered when `user` is `null` (no authenticated session).

### 3.3 UserMenu (new component)

- **REQ-020**: File MUST be created at `src/features/desktop/UserMenu.tsx`. It exports a single default React component with props:

  ```ts
  interface UserMenuProps {
    open: boolean;
    anchorRef: React.RefObject<HTMLElement>;
    user: User;
    isAdmin: boolean;
    currentWorkspaceName: string;
    onClose: () => void;
    onOpenSettings: () => void;
    onOpenAdmin: () => void;
    onLogout: () => void;
  }
  ```

- **REQ-021**: The popover MUST be 320 px wide on desktop. It MUST use `.glass-surface`, `border: 1px solid var(--border-dim)`, `border-radius: 12px`, `box-shadow: 0 16px 48px rgba(0,0,0,0.4)`, and the `modal-panel-in` keyframe for entry.
- **REQ-022**: The popover MUST contain, top to bottom:

  | Row | Content | Interaction |
  |---|---|---|
  | Header | Avatar (40 px) · `user.name` · `user.email` | None |
  | Section "Workspace" | `△ {currentWorkspaceName}` + trailing "Cambiar" button | Opens workspace-switch command (stub). |
  | Section "Preferencias" | Inline controls for language, theme (oscuro/claro/sistema), model (auto/opus/sonnet/haiku) | Each control updates preferences immediately via existing APIs. |
  | Link row | "Más ajustes →" | Calls `onOpenSettings()` and closes the menu. |
  | Admin row (conditional) | "🛡 Admin" | Visible only when `isAdmin === true`; calls `onOpenAdmin()`. |
  | Divider | — | — |
  | Destructive row | "Cerrar sesión" in `#ef4444` text | Calls `onLogout()`. |

- **REQ-023**: The popover MUST trap focus while open. First focused element on open: the first interactive row after the header (Workspace row).
- **REQ-024**: Keyboard behavior:
  - `Esc` closes the popover and returns focus to the Avatar Anchor.
  - `↑` / `↓` move focus between interactive rows (roving `tabindex`).
  - `Home` / `End` jump to first / last interactive row.
  - `Enter` / `Space` activates the focused row.
  - `Tab` / `Shift+Tab` wrap within the popover.
- **REQ-025**: The popover MUST close on: outside click, route change (`activeApp` change), window resize crossing the mobile breakpoint, or after any row action completes.
- **REQ-026**: The popover MUST be rendered via a React portal into `document.body` to escape rail `overflow` clipping.
- **REQ-027**: The popover MUST have `role="menu"` with child rows as `role="menuitem"`. The destructive "Cerrar sesión" row MUST additionally include `aria-describedby` pointing to a hidden span with Spanish/English text "Acción destructiva: cierra tu sesión".
- **SEC-001**: The logout row MUST call `logout()` from `AuthContext` exactly once per click. It MUST NOT double-fire. A click during an in-flight logout MUST be a no-op.
- **SEC-002**: No sensitive data (JWT, email contents beyond `user.email`) MUST be written to `localStorage` by this component.

### 3.4 CommandPalette Extensions

- **REQ-030**: `CommandPalette.tsx` MUST register a new command group labeled "Cuenta" (i18n-keyed `commandPalette.groups.account`) appearing after existing groups.
- **REQ-031**: The group MUST contain these commands in this order:

  | ID | Label (es) | Shortcut | Action |
  |---|---|---|---|
  | `account.openSettings` | "Abrir ajustes" | `⌘,` | Sets `activeApp='settings'`. |
  | `account.changeLanguage` | "Cambiar idioma" | none | Opens `LanguageSwitcher` (dispatches a custom window event `liwaisi:openLanguageSwitcher`). |
  | `account.switchWorkspace` | "Cambiar workspace" | none | Opens the workspace stub modal (disabled state with "Próximamente" copy). |
  | `account.billing` | "Ir a facturación" | none | Sets `activeApp='settings'` and scrolls to the billing section (stub: focuses the balance area). |
  | `account.logout` | "Cerrar sesión" | `⌘⇧Q` | Calls `logout()`. Confirmation: inline `aria-live` announcement "Cerrando sesión…" before redirect. |

- **REQ-032**: `⌘,` and `⌘⇧Q` MUST be registered globally (not just inside the palette) via the existing keyboard-shortcut hook.

### 3.5 DesktopLayout Wiring

- **REQ-040**: `DesktopLayout.tsx` MUST pass `user`, `isAdmin`, `onOpenSettings`, `onOpenAdmin`, and `onLogout` into `NavigationRail`. `NavigationRail` owns the `UserMenu` open/close state.
- **REQ-041**: `DesktopLayout.tsx` MUST stop passing `onOpenSettings` to `StatusBar`.
- **REQ-042**: `handleUserSettingsNav()` MUST remain as the shared `onOpenSettings` handler but is now invoked only from `UserMenu` and `CommandPalette`.

### 3.6 i18n

- **REQ-050**: The following keys MUST exist in both `src/i18n/locales/es/desktop.json` and `src/i18n/locales/en/desktop.json`:

  ```
  userMenu.openLabel
  userMenu.workspace.heading
  userMenu.workspace.switch
  userMenu.preferences.heading
  userMenu.preferences.language
  userMenu.preferences.theme
  userMenu.preferences.model
  userMenu.moreSettings
  userMenu.admin
  userMenu.logout
  userMenu.logoutDescription
  commandPalette.groups.account
  commandPalette.commands.openSettings
  commandPalette.commands.changeLanguage
  commandPalette.commands.switchWorkspace
  commandPalette.commands.billing
  commandPalette.commands.logout
  ```

- **REQ-051**: Spanish copy is authoritative. English translations MUST be provided but short and action-oriented.

### 3.7 Design System Guidelines

- **GUD-001**: Reuse `.glass-surface`, `var(--accent)`, `var(--border-dim)`, JetBrains Mono — do not introduce new tokens.
- **GUD-002**: Icon sizes: 16 px for inline row icons; 20 px for section-heading icons; 40 px for the header avatar inside the popover; 32 px for the Avatar Anchor in the rail.
- **GUD-003**: All new click targets MUST be ≥ 36 × 36 px hit area (may be smaller visually with padding).
- **GUD-004**: All new text MUST meet WCAG 2.2 AA contrast against its background (verify `--text-secondary` #94a3b8 on `--bg-surface` #12121a ≥ 4.5:1 for body; it is 5.1:1 — OK).
- **GUD-005**: Animations MUST respect `prefers-reduced-motion: reduce` — skip `modal-panel-in`, skip rail width transition, use `opacity` instant swaps.

### 3.8 Patterns to Follow

- **PAT-001**: Mirror `LanguageSwitcher.tsx`'s structure: trigger + portal popover + `useEffect` click-outside + focus hook. Do NOT introduce Radix, shadcn, or Headless UI.
- **PAT-002**: Use existing `useTooltip(200)` for the Avatar Anchor's tooltip when the rail is collapsed.
- **PAT-003**: Persist rail expansion with the same approach used (if any) by `ChatSidebar`; otherwise use a small `useLocalStorage<boolean>` hook local to `NavigationRail`.

### 3.9 Constraints

- **CON-010**: No new NPM dependencies. Net dependency delta MUST be zero.
- **CON-011**: Bundle size impact MUST be ≤ 6 KB gzipped for all new code combined.
- **CON-012**: No URL router is introduced. `activeApp` remains the navigation state.
- **CON-013**: No changes to backend APIs, SSE protocol, or `AuthContext` contract.

## 4. Interfaces & Data Contracts

### 4.1 UserMenu Props

```ts
import type { User } from '@/contexts/AuthContext';

export interface UserMenuProps {
  open: boolean;
  anchorRef: React.RefObject<HTMLElement>;
  user: User;
  isAdmin: boolean;
  currentWorkspaceName: string;
  onClose: () => void;
  onOpenSettings: () => void;
  onOpenAdmin: () => void;
  onLogout: () => void;
}
```

### 4.2 NavigationRail Prop Additions

```ts
interface NavigationRailProps {
  // existing...
  user: User | null;
  isAdmin?: boolean;
  onOpenSettings: () => void;
  onOpenAdmin?: () => void;
  onLogout: () => void;
}
```

### 4.3 localStorage Contract

| Key | Type | Values | Purpose |
|---|---|---|---|
| `liwaisi_rail_expanded` | `string` | `"1"` \| `"0"` | Persist rail expansion. |

### 4.4 Custom Window Events

| Event | Detail | Fired by | Consumed by |
|---|---|---|---|
| `liwaisi:openLanguageSwitcher` | `undefined` | `CommandPalette` action | `LanguageSwitcher` (adds a listener to auto-open). |

### 4.5 StatusBar Props (after change)

```ts
interface StatusBarProps {
  sessionState: SessionState;
  isConnected: boolean;
  activeApp?: ActiveApp;
  personalityPrinciples?: PersonalityBadge[];
  // onOpenSettings REMOVED
}
```

## 5. Acceptance Criteria

- **AC-001**: Given an authenticated desktop user, When the app loads, Then the header `StatusBar` displays no avatar image, no user name, no "Configuración" button, and no "Cerrar sesión" button.
- **AC-002**: Given the desktop shell, When the user clicks the Avatar Anchor at the bottom of the `NavigationRail`, Then the `UserMenu` popover opens with focus placed on the Workspace row.
- **AC-003**: Given the `UserMenu` is open, When the user presses `Esc`, Then the popover closes and focus returns to the Avatar Anchor.
- **AC-004**: Given the `UserMenu` is open, When the user presses `↓` twice from the first row, Then focus moves to the second interactive row and then the third.
- **AC-005**: Given the `UserMenu` is open, When the user clicks the "Cerrar sesión" row, Then `logout()` from `AuthContext` is invoked exactly once and the user is redirected to the landing page.
- **AC-006**: Given the `UserMenu` is open, When the user clicks outside the popover, Then the popover closes without firing any action.
- **AC-007**: Given `user.is_admin === false`, When the `UserMenu` is rendered, Then the Admin row is absent from the DOM.
- **AC-008**: Given the rail is expanded, When the user reloads the page, Then the rail hydrates in the expanded state without a visible collapse-to-expand flash.
- **AC-009**: Given the user presses `⌘K` and types "cerrar", Then the "Cerrar sesión" command appears under the "Cuenta" group and invoking it logs the user out.
- **AC-010**: Given `prefers-reduced-motion: reduce`, When the popover opens, Then no CSS transform/opacity keyframe plays.
- **AC-011**: Given viewport width < 480 px, When the Avatar Anchor is clicked, Then the menu renders as a bottom sheet, not a side popover.
- **AC-012**: Given the rail is collapsed, When the pointer hovers the Avatar Anchor for ≥ 200 ms, Then a tooltip reading the i18n value of `userMenu.openLabel` appears to the right of the anchor.
- **AC-013**: The production bundle delta for the `main` chunk MUST be ≤ 6 KB gzipped vs. the previous `main` branch build.
- **AC-014**: `axe-core` automated scan on the open popover returns zero serious or critical violations.

## 6. Test Automation Strategy

- **Test Levels**: Unit (component render + interaction), Integration (shell shell+menu+palette together), Manual QA (dev server walkthrough).
- **Frameworks**: Vitest + `@testing-library/react` + `@testing-library/jest-dom` (already in `package.json`).
- **Test Files**:
  - `src/features/desktop/__tests__/UserMenu.test.tsx` — render, open/close, keyboard nav, logout, admin conditional, portal rendering, `prefers-reduced-motion`.
  - `src/features/desktop/__tests__/StatusBar.test.tsx` — assert absence of removed elements; retained elements still render.
  - `src/features/desktop/__tests__/NavigationRail.test.tsx` — Avatar Anchor renders, localStorage hydration, tooltip on collapsed state, menu open toggles `aria-expanded`.
  - `src/features/desktop/__tests__/CommandPalette.accountCommands.test.tsx` — new commands registered, firing logout command calls `logout`.
- **Test Data Management**: Mock `useAuth` with a factory returning `user: { name, email, picture, is_admin }` and a `vi.fn()` `logout`.
- **CI/CD Integration**: Existing Vitest config runs on pre-commit hook; no pipeline change needed.
- **Coverage Requirements**: ≥ 85 % line coverage on `UserMenu.tsx`. Shell components previously untested are exempted.
- **Performance Testing**: Verify React profiler shows `UserMenu` open cost ≤ 8 ms on a mid-tier laptop; rail hydration before first paint (no `useEffect`-driven flash).

## 7. Rationale & Context

Current shell spreads configuration over three surfaces (header "Configuración", header "Cerrar sesión", rail gear "Identidad") plus a separate `SettingsPage`. Users must recognize three different visual affordances to complete the same mental intent: "manage my account". This violates the 2026 SaaS shell convention (Linear, Vercel, Raycast) of a single avatar-anchored menu.

Moving the anchor from header to rail matches the pattern because:

1. The rail is **always visible** and **spatially stable** — the avatar lives in a position the user already looks at for navigation.
2. The header becomes ambient (read-only status), reducing cognitive load and freeing horizontal space for session context.
3. A rail-anchored avatar scales gracefully to touch — the Avatar Anchor becomes the obvious entry point for a bottom sheet on narrow viewports.

The Command Palette path is added because menus do not scale past ~10 actions. Registering account commands in `⌘K` gives power users O(1) access without traversing the popover.

`localStorage`-backed rail expansion honors user intent across reloads — a recurring finding from Sidebar UX studies: resetting sidebar state on every load is perceived as "fighting the user".

## 8. Dependencies & External Integrations

### External Systems

- **EXT-001**: Google Identity (existing) — surfaces `user.picture`, `user.name`, `user.email`, and is the authority for `logout()` side effects.

### Third-Party Services

- None added by this spec.

### Infrastructure Dependencies

- **INF-001**: Browser `localStorage` — required for rail state persistence. Fallback behavior: if unavailable (private mode, quota exceeded), the rail defaults to collapsed and the popover menu operates normally.

### Data Dependencies

- **DAT-001**: `AuthContext.user` object — MUST provide `{ name, email, picture, is_admin }`. No new fields required.
- **DAT-002**: Preferences API (existing) — `updatePreferences({ language, theme, model })` MUST be callable from the popover.

### Technology Platform Dependencies

- **PLT-001**: React 19 portals (`createPortal`) — required for popover rendering.
- **PLT-002**: Tailwind v4 utility classes — existing.
- **PLT-003**: `prefers-reduced-motion` media query support — all target browsers pass.

### Compliance Dependencies

- **COM-001**: WCAG 2.2 AA — new interactive elements MUST meet contrast, focus-visible, and keyboard operability requirements.

## 9. Examples & Edge Cases

### 9.1 Minimal UserMenu usage (in NavigationRail)

```tsx
const anchorRef = useRef<HTMLButtonElement>(null);
const [menuOpen, setMenuOpen] = useState(false);

return (
  <>
    <button
      ref={anchorRef}
      aria-haspopup="menu"
      aria-expanded={menuOpen}
      aria-controls="user-menu-popover"
      onClick={() => setMenuOpen((v) => !v)}
    >
      {/* avatar image or monogram */}
    </button>
    <UserMenu
      open={menuOpen}
      anchorRef={anchorRef}
      user={user}
      isAdmin={user.is_admin}
      currentWorkspaceName="Liwaisi Tech"
      onClose={() => setMenuOpen(false)}
      onOpenSettings={() => { setMenuOpen(false); onOpenSettings(); }}
      onOpenAdmin={() => { setMenuOpen(false); onOpenAdmin?.(); }}
      onLogout={() => { setMenuOpen(false); onLogout(); }}
    />
  </>
);
```

### 9.2 Rail expansion hydration without flash

```tsx
const [isExpanded, setIsExpanded] = useState<boolean>(() => {
  if (typeof window === 'undefined') return false;
  try {
    return window.localStorage.getItem('liwaisi_rail_expanded') === '1';
  } catch {
    return false;
  }
});

useEffect(() => {
  try {
    window.localStorage.setItem('liwaisi_rail_expanded', isExpanded ? '1' : '0');
  } catch {
    /* storage disabled — silently degrade */
  }
}, [isExpanded]);
```

### 9.3 Edge cases

| Case | Expected behavior |
|---|---|
| `user.picture` 404s | Avatar renders monogram fallback; no console error propagated. |
| `user === null` mid-session (token expired) | Avatar Anchor unmounts; existing `auth:expired` handler routes to landing. |
| Popover open when user clicks rail nav icon | Popover closes; navigation proceeds. |
| Language changed inside popover | Popover closes after change; `LanguageSwitcher` global event not re-fired. |
| User presses `⌘⇧Q` inside an input | Logout fires (global shortcut); the input does not swallow it — tests MUST cover this. |
| Very long `user.name` (≥ 40 chars) | Truncated with CSS `text-overflow: ellipsis` in header row; full name in `title` attribute. |
| Narrow viewport (`< 480 px`) with popover open, then resize wider | Popover closes; the user re-opens to get the desktop side-popover variant. |

## 10. Validation Criteria

1. All ACs in §5 pass in the dev server and in Vitest.
2. The spec's required i18n keys exist in both locales with non-empty values.
3. `grep -R "Configuración" front/react-assistant/src/features/desktop/StatusBar.tsx` returns no matches (the header no longer contains the label).
4. `grep -R "Cerrar sesión" front/react-assistant/src/features/desktop/StatusBar.tsx` returns no matches.
5. `grep -R "logout" front/react-assistant/src/features/desktop/StatusBar.tsx` returns no matches.
6. The `UserMenu.tsx` file exists and exports a default component with the contract in §4.1.
7. Bundle-size check (`vite build` output) reports `main` delta ≤ 6 KB gzipped vs. the `main` branch baseline.
8. `axe` automated scan on the shell with `UserMenu` open reports zero serious/critical violations.
9. Manual smoke test: open popover → change language → reload page → rail expansion persists → `⌘K` → "Cerrar sesión" → lands on landing page.

## 11. Related Specifications / Further Reading

- [`spec-design-ux-refresh-navigation.md`](./spec-design-ux-refresh-navigation.md) — original rail + palette + adaptive-panel spec. Portions superseded by this document for the header/account area.
- [`spec-design-user-preferences-settings-page.md`](./spec-design-user-preferences-settings-page.md) — target of the "Más ajustes" link.
- [`spec-design-i18n-internationalization.md`](./spec-design-i18n-internationalization.md) — i18n key conventions.
- [`spec-architecture-google-oauth-login.md`](./spec-architecture-google-oauth-login.md) — identity source for the avatar and logout action.
- External: Vercel — *New dashboard navigation* changelog (Feb 2026).
- External: Linear command palette patterns; Raycast extension menu patterns.
- External: WCAG 2.2 (W3C, 2023) and WAI-ARIA Authoring Practices Guide — Menu and Menu Button patterns.
