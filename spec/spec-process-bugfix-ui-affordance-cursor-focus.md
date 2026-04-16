---
title: "Bug Fix — UI Affordance: Cursor Pointer & Focus-Visible on Logged-in Interface"
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: liwaisi-tech
tags: [process, bugfix, frontend, ux, a11y, tailwind-v4, react]
---

# Introduction

The logged-in chat interface of the React frontend
(`front/react-assistant`) exposes interactive controls — header
"Configuration" and "Logout" buttons, the vertical navigation rail icons,
the sidebar toggle, chat-list actions, context-menu items, and the chat
input "Send" button — that **do not change the cursor to a pointer on
hover** and, in most cases, provide **no visible focus ring** for
keyboard users. Users perceive these elements as non-interactive even
when hover styles exist, and keyboard operators cannot track focus at
all.

The root cause is a single regression introduced by the Tailwind CSS v4
upgrade. Tailwind v4 Preflight changed the default cursor of `<button>`
elements from `cursor: pointer` (v3 behaviour) to `cursor: default`
(matching browser UA defaults). Every interactive element in the
logged-in UI relied on the v3 default and therefore lost its pointer
affordance. A secondary defect layered on top: multiple components
handle hover via inline `onMouseEnter` / `onMouseLeave` React state
instead of CSS `:hover`, which means the cursor never responds to hover
at all on those elements, regardless of Preflight behaviour.

This specification defines a narrow, global-first fix. It introduces no
new components, no new design tokens, and no behavioural changes. It
restores the v3-equivalent pointer affordance via a single global CSS
rule, converts inline mouse handlers to CSS `:hover`, adds a shared
`focus-visible` ring for keyboard accessibility, and corrects one
semantic defect (`<div onClick>` used as a list-item button).

## 1. Purpose & Scope

**Purpose**: Restore correct `cursor: pointer` behaviour on all
enabled interactive controls of the logged-in React interface, add a
visible `focus-visible` indicator for keyboard navigation, and remove
inline JS hover styling in favor of CSS on the affected components.

**In scope**:
- Global CSS rule in
  `front/react-assistant/src/index.css` that restores
  `cursor: pointer` for `<button>`, `[role="button"]`, `<a>` with
  `href`, `<summary>`, and form `label[for]` elements — excluding
  disabled and `aria-disabled="true"` elements.
- Global CSS rule for a subtle `:focus-visible` ring on the same
  interactive elements, keyed to `--accent`.
- `ChatHeader.tsx` — replace inline `onMouseEnter` / `onMouseLeave`
  hover handling for Configuration and Logout buttons with CSS
  `:hover` classes; add explicit `cursor-pointer` where missing.
- `MessageInput.tsx` — add explicit `hover:` state and rely on global
  cursor rule for the Send button.
- `NavigationRail.tsx` — confirm all four nav buttons (Chat, Tools,
  Admin, Settings) receive cursor via global rule; add
  `focus-visible` classes if global rule does not cover their
  selector.
- `ChatSidebar.tsx` — (a) convert the chat-list item `<div onClick>`
  to a semantic `<button>` or apply `role="button"` + `tabIndex={0}`
  + keyboard activation handler; (b) ensure toggle, rename input,
  action menu, and context-menu buttons get cursor + focus-visible.
- `StatusBar.tsx` — replace inline mouse handlers for Configuration
  and Logout with CSS `:hover`; add cursor + focus-visible.
- Unit / component tests that assert the critical interactive
  elements render with `cursor: pointer` and the expected
  `focus-visible` outline applied under JSDOM + simulated keyboard
  focus.

**Out of scope**:
- Redesign of any component, colour palette, or layout.
- Replacement of `tailwindcss` v4 with v3 or any alternative CSS
  framework.
- Tooltips, aria-labels, or copy improvements on icon-only buttons
  (tracked as P2 in the panel audit, addressed in a separate spec).
- Responsive/mobile-specific layout changes.
- Changes to any unlogged / landing-page surfaces (`Landing*`,
  sign-in modal). Their hover affordances already rely on
  `.suggestion-chip`, `.footer-link`, etc., which are explicit CSS
  selectors unaffected by the v4 default change.
- Adding Enter-to-send discoverability copy or other interaction
  hints.
- Consolidating duplicated Config/Logout UI between `ChatHeader`
  and `StatusBar`.

**Intended audience**: Frontend engineers implementing the fix,
reviewers validating the PR, and downstream QA.

## 2. Definitions

- **Preflight**: Tailwind CSS's base style layer; the set of default
  element styles injected by `@import "tailwindcss";`.
- **v3 → v4 cursor change**: Tailwind v4 Preflight sets
  `button { cursor: default; }` (matching browser UA), whereas v3 set
  `button { cursor: pointer; }`. See Tailwind v4 upgrade guide,
  section "Buttons use the default cursor".
- **Affordance**: A visual signal that an element is interactive — in
  this context, the hand/pointer cursor and visible focus outline.
- **`:focus-visible`**: CSS pseudo-class that matches an element only
  when the user agent determines focus should be visually indicated —
  typically during keyboard navigation but not mouse clicks.
- **P0/P1**: Priority tiers from the UX audit. P0 = reported bugs
  (cursor affordance). P1 = keyboard a11y gaps bundled into this fix.
- **NavigationRail**: The vertical icon menu on the left edge of the
  logged-in desktop shell
  (`src/features/desktop/NavigationRail.tsx`).
- **ChatHeader**: The top header bar inside the chat surface
  containing the Configuration and Logout buttons
  (`src/features/chat/ChatHeader.tsx`).
- **StatusBar**: Bottom status bar in the desktop shell, which also
  renders Configuration/Logout controls
  (`src/features/desktop/StatusBar.tsx`).
- **MessageInput**: Chat composer with the Send button
  (`src/features/chat/MessageInput.tsx`).
- **ChatSidebar**: Conversations list in the chat surface
  (`src/features/chat/ChatSidebar.tsx`).

## 3. Requirements, Constraints & Guidelines

### Requirements

- **REQ-001**: All enabled `<button>` elements rendered within the
  logged-in route tree MUST display `cursor: pointer` on hover.
- **REQ-002**: All elements with `role="button"` that are not
  `aria-disabled="true"` MUST display `cursor: pointer` on hover.
- **REQ-003**: `<a>` elements that carry an `href` attribute MUST
  display `cursor: pointer` on hover.
- **REQ-004**: `button[disabled]` and `[aria-disabled="true"]` MUST
  display `cursor: not-allowed` (or retain the Tailwind-provided
  `disabled:cursor-not-allowed` where already applied) — i.e., the
  global rule MUST NOT override disabled-state cursors.
- **REQ-005**: All interactive elements listed in REQ-001..REQ-003
  MUST show a visible focus indicator when focused via keyboard
  (`:focus-visible`), using the project accent colour
  (`var(--accent)`).
- **REQ-006**: The focus indicator MUST NOT appear on mouse-only
  focus (click-to-focus), to avoid visual noise for pointer users.
  Use `:focus-visible`, not `:focus`.
- **REQ-007**: `ChatHeader` Configuration and Logout buttons MUST use
  CSS `:hover` for colour changes; inline `onMouseEnter` /
  `onMouseLeave` handlers for hover styling MUST be removed.
- **REQ-008**: `StatusBar` Configuration and Logout buttons MUST use
  CSS `:hover` for colour changes; inline `onMouseEnter` /
  `onMouseLeave` handlers for hover styling MUST be removed.
- **REQ-009**: `MessageInput` Send button MUST have an explicit
  `hover:` background state in its enabled form.
- **REQ-010**: The chat-list item in `ChatSidebar.tsx:83-91`,
  currently a `<div onClick>`, MUST be converted to a semantic
  `<button>` OR MUST receive `role="button"`, `tabIndex={0}`, and a
  keyboard activation handler (`onKeyDown` that triggers on `Enter`
  and `Space`).
- **REQ-011**: All changes MUST preserve existing behaviour for
  pointer users — current click targets, click handlers, disabled
  states, and visual states other than cursor/focus MUST remain
  identical.
- **REQ-012**: A regression test MUST assert that a representative
  set of interactive elements (at minimum: Send button, Logout
  button, Configuration button, NavigationRail Chat icon) render
  with `cursor: pointer` in the enabled state.

### Security Requirements

- **SEC-001**: No new event handlers MUST be added beyond those
  required by REQ-010's keyboard activation. The fix MUST NOT
  introduce any new code path that reads or writes user data,
  session tokens, or remote state.
- **SEC-002**: The `<div>` → `<button>` conversion in REQ-010 MUST
  NOT cause any `form` enclosing it to submit by accident — the new
  element MUST either be outside any `<form>` (current state) or
  carry `type="button"`.

### Accessibility Requirements

- **A11Y-001**: The new `:focus-visible` style MUST meet WCAG 2.1
  Success Criterion 2.4.7 (Focus Visible). The indicator MUST be at
  least 2 px thick or provide an equivalent non-dashed outline
  offset.
- **A11Y-002**: The new `:focus-visible` style MUST maintain a
  contrast ratio of at least 3:1 between the indicator and the
  adjacent background per WCAG 2.2 SC 1.4.11 (Non-text Contrast).
  `var(--accent) = #0ea5e9` against `var(--bg-surface) = #12121a`
  satisfies this (~7.2:1).
- **A11Y-003**: Keyboard users MUST be able to activate the
  converted `ChatSidebar` chat-list item via `Enter` and `Space`.

### Constraints

- **CON-001**: The fix MUST work with Tailwind CSS v4 Preflight
  without downgrading.
- **CON-002**: The global CSS rule MUST live inside
  `front/react-assistant/src/index.css` and be scoped via selector
  specificity alone — do NOT use `!important` unless a specific
  Tailwind utility (e.g., `disabled:cursor-not-allowed`) is being
  overridden by cascade order, in which case target the minimum
  required selector.
- **CON-003**: The global rule MUST NOT apply `cursor: pointer` to
  elements that are plain containers styled as clickable via
  JavaScript only (e.g., `<div onClick>` without `role="button"`) —
  these MUST be fixed at the component level to preserve semantic
  correctness.
- **CON-004**: No existing component public prop signatures may
  change. Internal changes only.
- **CON-005**: All new code MUST pass the project's existing lint,
  type-check, and test gates (`npm run lint`, `npm run typecheck`,
  `npm test` or equivalent commands defined in `package.json`).

### Guidelines

- **GUD-001**: Prefer the global CSS approach over per-component
  `cursor-pointer` class sprinkling. Global restoration is the
  official Tailwind v4 workaround, minimises diff churn, and keeps
  future components correct by default.
- **GUD-002**: When converting inline hover handlers to CSS, remove
  the corresponding `useState` / `useCallback` pairs if they no
  longer serve another purpose. Do not leave dead code.
- **GUD-003**: Use Tailwind's `focus-visible:` variant for
  per-component overrides if needed. The global `:focus-visible`
  rule provides the baseline; components may opt in to a stronger
  ring when the baseline is insufficient.
- **GUD-004**: Keep the `index.css` addition colocated with existing
  "Base resets" block (around line 20). Add a clear section comment
  (`/* -- Interactive affordance (Tailwind v4 Preflight restore) -- */`).

### Patterns to follow

- **PAT-001**: Global rule template:

  ```css
  /* -- Interactive affordance (Tailwind v4 Preflight restore) -- */
  button:not(:disabled):not([aria-disabled="true"]),
  [role="button"]:not([aria-disabled="true"]),
  a[href]:not([aria-disabled="true"]),
  summary,
  label[for] {
    cursor: pointer;
  }

  button:not(:disabled):focus-visible,
  [role="button"]:not([aria-disabled="true"]):focus-visible,
  a[href]:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: inherit;
  }
  ```

- **PAT-002**: Component hover conversion template — replace:

  ```tsx
  const [hover, setHover] = useState(false);
  <button
    onMouseEnter={() => setHover(true)}
    onMouseLeave={() => setHover(false)}
    style={{ color: hover ? 'var(--accent)' : 'var(--text-secondary)' }}
  />
  ```

  with:

  ```tsx
  <button className="text-[var(--text-secondary)] hover:text-[var(--accent)] transition-colors" />
  ```

  (or the CSS-variable-aware Tailwind equivalent already used in
  that file).

- **PAT-003**: `<div>` → `<button>` conversion template — prefer a
  native `<button type="button">`. Only fall back to `role="button"`
  if existing layout (e.g., nested interactive elements, grid
  semantics) would break.

## 4. Interfaces & Data Contracts

This is a presentational-only change. No API, no data contract, no
backend touchpoint is modified.

### Affected files

| File | Role | Change type |
|---|---|---|
| `front/react-assistant/src/index.css` | Global CSS | Append cursor + focus-visible rules |
| `front/react-assistant/src/features/chat/ChatHeader.tsx` | Component | Replace inline hover handlers with CSS classes |
| `front/react-assistant/src/features/chat/MessageInput.tsx` | Component | Add `hover:` background on Send button |
| `front/react-assistant/src/features/chat/ChatSidebar.tsx` | Component | Convert `<div onClick>` to `<button>`; confirm focus visibility |
| `front/react-assistant/src/features/desktop/NavigationRail.tsx` | Component | Add `focus-visible:` classes if global rule insufficient |
| `front/react-assistant/src/features/desktop/StatusBar.tsx` | Component | Replace inline hover handlers with CSS classes |
| `front/react-assistant/src/features/chat/__tests__/*.test.tsx` | Tests | Add regression tests per REQ-012 |

### CSS selectors affected by global rule

```
button:not(:disabled):not([aria-disabled="true"])
[role="button"]:not([aria-disabled="true"])
a[href]:not([aria-disabled="true"])
summary
label[for]
```

## 5. Acceptance Criteria

- **AC-001**: **Given** the app is open and the user is logged in,
  **When** the user hovers the "Logout" button in `ChatHeader`,
  **Then** the cursor displays as a pointer and the button text
  colour animates via CSS `:hover`.
- **AC-002**: **Given** the app is open and the user is logged in,
  **When** the user hovers the "Configuration" button in
  `ChatHeader`, **Then** the cursor displays as a pointer and the
  button background animates via CSS `:hover`.
- **AC-003**: **Given** the user is on the chat view, **When** the
  user hovers the "Send" paper-plane button in the message composer
  with a non-empty message, **Then** the cursor displays as a
  pointer.
- **AC-004**: **Given** the user is on the chat view, **When** the
  "Send" paper-plane button is disabled (empty input or pending
  state), **Then** the cursor displays as `not-allowed`, NOT as
  `pointer`.
- **AC-005**: **Given** the user is on the chat view, **When** the
  user hovers any icon in the vertical `NavigationRail` (Chat,
  Tools, Admin, Settings), **Then** the cursor displays as a
  pointer.
- **AC-006**: **Given** the user is on the chat view, **When** the
  user hovers the sidebar toggle, a chat-list item, a chat-list
  action button, or a context-menu entry, **Then** the cursor
  displays as a pointer.
- **AC-007**: **Given** the user navigates by keyboard, **When**
  focus lands on any of the interactive elements named in
  REQ-001..REQ-003, **Then** a visible outline in the accent colour
  appears around the element.
- **AC-008**: **Given** a pointer user clicks an interactive
  element, **When** the click completes, **Then** no focus ring is
  visible (the element uses `:focus-visible`, not `:focus`).
- **AC-009**: **Given** the `ChatSidebar` chat-list item is focused
  via keyboard, **When** the user presses `Enter` OR `Space`,
  **Then** the chat is selected — identical to the mouse-click
  behaviour.
- **AC-010**: **Given** a component test harness rendering
  `MessageInput` with a non-empty value, **When** the test queries
  the computed style of the Send button, **Then** `cursor` equals
  `pointer`.
- **AC-011**: **Given** a component test harness rendering
  `ChatHeader` with an authenticated user, **When** the test
  queries the computed style of the Logout and Configuration
  buttons, **Then** `cursor` equals `pointer` for both.
- **AC-012**: **Given** the project test suite, **When** the full
  suite is run after the fix, **Then** all existing tests continue
  to pass (no regressions).

## 6. Test Automation Strategy

- **Test Levels**:
  - Unit / component tests via **Vitest** + **@testing-library/react**
    + **jsdom** (matching the existing setup in
    `vitest.config.ts`).
  - Manual smoke test in a running dev server (`npm run dev`) to
    validate cursor and focus visually — jsdom does not render
    actual cursors.
- **Frameworks**: Vitest (existing), @testing-library/react
  (existing), @testing-library/user-event for keyboard simulation
  (add if missing).
- **Test Data Management**: Use existing component fixtures. The
  regression tests MUST mock only what the component under test
  already mocks (session context, i18n, routing).
- **CI/CD Integration**: Tests run via the existing
  `npm run test` target invoked from the project's CI pipeline. No
  new CI job is required.
- **Coverage Requirements**: The three added regression tests MUST
  cover every acceptance criterion that is observable in jsdom
  (AC-010, AC-011, and a keyboard-activation test for AC-009).
  Overall project coverage MUST NOT regress below its current
  baseline.
- **Performance Testing**: Not applicable. CSS-only affordance
  changes have no measurable performance impact.
- **Manual verification checklist** (required before PR merge):
  1. Run `npm run dev`. Log in.
  2. Hover every control in the header, nav rail, sidebar, and
     composer. Confirm pointer cursor on every enabled control and
     `not-allowed` on disabled ones.
  3. Tab through the logged-in UI. Confirm a visible focus ring
     lands on every interactive element in tab order.
  4. Click any button with the mouse. Confirm no focus ring
     remains after the click (i.e., `:focus-visible` is honoured).
  5. Focus the `ChatSidebar` chat-list item with keyboard, press
     `Enter`, confirm selection fires. Repeat with `Space`.
  6. Confirm the Send button shows `cursor: not-allowed` when the
     composer is empty.

## 7. Rationale & Context

### Why a global CSS rule instead of per-component `cursor-pointer`

The symptom is systemic because the cause is systemic: one Tailwind
v4 Preflight default changed, silently affecting every `<button>` in
the app. Patching ~20 component files with `cursor-pointer` utility
classes would:

1. Produce a large, noisy diff with no protection against future
   components forgetting the utility.
2. Diverge from the pattern Tailwind itself recommends as the v4
   migration workaround.
3. Require re-auditing on every future component.

A single scoped CSS rule in `index.css` matches the exact scope of
the regression — elements that the browser itself semantically marks
as interactive. It fixes today's bugs and prevents tomorrow's.

### Why `:focus-visible`, not `:focus`

`:focus` shows an outline whenever an element is focused — including
immediately after a mouse click. Most users perceive this as visual
noise ("why is there a ring around the button I just clicked?").
`:focus-visible` defers to the user agent's heuristic, which shows
the outline for keyboard focus and withholds it for pointer focus.
This is the WAI-ARIA APG recommended behaviour.

### Why convert the chat-list `<div>` to a `<button>`

The chat-list item in `ChatSidebar.tsx:83-91` is effectively a
button — it has an `onClick`, and user expectation is that
`Enter` / `Space` select a conversation. Today it is a `<div>` and is
therefore:

- Skipped by tab-ordered keyboard navigation.
- Invisible to assistive tech as an interactive element.
- Ineligible for the global cursor rule (see CON-003), because
  extending that rule to `<div>` would incorrectly mark every
  container as clickable.

Fixing the semantic, rather than broadening the CSS rule, is the
correct design.

### Why keep inline mouse handlers out of `ChatHeader` / `StatusBar`

State-driven hover has three costs:

1. It re-renders the component on every mouse entry/exit.
2. It does not respect the cursor — CSS `:hover` and the cursor are
   decoupled from React state, so users never see a pointer unless
   the element independently has `cursor: pointer`.
3. It does not apply when the element gains focus by keyboard, so
   keyboard users never see the "hover" styling.

CSS `:hover` + `:focus-visible` is cheaper, more consistent, and
keyboard-accessible by default.

## 8. Dependencies & External Integrations

### External Systems
- None. This fix has no external system touchpoints.

### Third-Party Services
- None.

### Infrastructure Dependencies
- **INF-001**: The production bundle is served by the existing
  `nginx.conf` in `front/react-assistant/`. No nginx change
  required; the CSS is bundled by Vite.

### Data Dependencies
- None.

### Technology Platform Dependencies
- **PLT-001**: Tailwind CSS v4 (`tailwindcss ^4.0.0`,
  `@tailwindcss/vite ^4.0.0`). The global CSS rule restores v3
  cursor behaviour without downgrading.
- **PLT-002**: Modern evergreen browsers (Chromium ≥ 86, Firefox ≥
  85, Safari ≥ 15.4) — required for `:focus-visible` without a
  polyfill. This matches the project's current browserslist.
- **PLT-003**: React 18+ (existing) for the component-level
  refactors.
- **PLT-004**: Vitest + jsdom (existing) for the regression tests.

### Compliance Dependencies
- **COM-001**: WCAG 2.1 AA — SC 2.4.7 (Focus Visible) and SC 1.4.11
  (Non-text Contrast). The focus-visible ring is the primary
  mechanism for compliance.

## 9. Examples & Edge Cases

### Example 1 — Global CSS addition

Append to `front/react-assistant/src/index.css` directly after the
existing `/* -- Base resets -- */` block (around line 28):

```css
/* -- Interactive affordance (Tailwind v4 Preflight restore) -- */
button:not(:disabled):not([aria-disabled="true"]),
[role="button"]:not([aria-disabled="true"]),
a[href]:not([aria-disabled="true"]),
summary,
label[for] {
  cursor: pointer;
}

/* Keyboard-only focus ring, using the project accent colour */
button:not(:disabled):focus-visible,
[role="button"]:not([aria-disabled="true"]):focus-visible,
a[href]:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: inherit;
}
```

### Example 2 — ChatHeader Logout button

**Before** (illustrative; exact code may vary):

```tsx
const [hover, setHover] = useState(false);

<button
  className="text-xs px-2 py-1 rounded transition-colors"
  style={{ color: hover ? 'var(--accent)' : 'var(--text-secondary)' }}
  onMouseEnter={() => setHover(true)}
  onMouseLeave={() => setHover(false)}
  onClick={onLogout}
>
  Logout
</button>
```

**After**:

```tsx
<button
  type="button"
  className="text-xs px-2 py-1 rounded transition-colors
             text-[var(--text-secondary)]
             hover:text-[var(--accent)]"
  onClick={onLogout}
>
  Logout
</button>
```

The pointer cursor now comes from the global rule, the hover colour
from CSS, and the focus ring from the global `:focus-visible` rule.

### Example 3 — ChatSidebar chat-list item

**Before**:

```tsx
<div
  className="... cursor-pointer"
  onClick={() => selectChat(id)}
>
  {title}
</div>
```

**After**:

```tsx
<button
  type="button"
  className="... text-left"     // buttons default to center alignment
  onClick={() => selectChat(id)}
>
  {title}
</button>
```

Keyboard activation (`Enter` / `Space`) is native to `<button>`; no
`onKeyDown` needed.

### Edge case 1 — Disabled Send button

When the composer is empty, the Send button carries `disabled`. The
global rule's selector `button:not(:disabled)` intentionally does
not match; Tailwind's `disabled:cursor-not-allowed` utility (already
present in `MessageInput.tsx:63-77`) applies. Expected cursor:
`not-allowed`.

### Edge case 2 — `aria-disabled="true"` without `disabled`

Some accessible components use `aria-disabled="true"` instead of the
HTML `disabled` attribute to preserve focusability for screen
readers. The global rule excludes these via
`:not([aria-disabled="true"])`. Expected cursor: browser default
(effectively `default`). Components that want an explicit
`not-allowed` cursor in this state MUST apply it themselves.

### Edge case 3 — Anchor without `href`

`<a>` without `href` is not interactive in HTML semantics. The
global rule only targets `a[href]`, so bare `<a>` elements are
unaffected. Expected cursor: `default`.

### Edge case 4 — Nested interactive elements

If a `<button>` contains an inner `<button>` (generally invalid HTML
but sometimes present in dropdown patterns), both receive
`cursor: pointer` from the global rule. No change in behaviour vs.
v3.

### Edge case 5 — `prefers-reduced-motion`

The focus-visible outline uses `outline`, not a transition. No
motion is added, so `prefers-reduced-motion: reduce` is unaffected.
The existing block at `index.css:444-449` continues to govern
animated elements.

## 10. Validation Criteria

A pull request satisfying this specification MUST:

1. Contain the global CSS block from Example 1, with the section
   comment, in `front/react-assistant/src/index.css`.
2. Remove all inline `onMouseEnter` / `onMouseLeave` hover-styling
   state from `ChatHeader.tsx` and `StatusBar.tsx` (a grep for
   `onMouseEnter` in these two files MUST return zero matches after
   the change, unless the handler serves a non-hover purpose).
3. Contain an explicit `hover:` utility on the `MessageInput` Send
   button in its enabled path.
4. Convert the `ChatSidebar.tsx:83-91` `<div onClick>` to a
   `<button type="button">` (preferred) or to a `<div>` with
   `role="button"`, `tabIndex={0}`, and `onKeyDown` handling
   `Enter` and `Space`.
5. Pass `npm run lint`, `npm run typecheck`, and `npm test` with no
   new warnings or errors.
6. Include at least three new Vitest cases verifying AC-009,
   AC-010, and AC-011.
7. Leave unchanged:
   - Every file outside the "Affected files" table in §4.
   - Every public prop or exported API from the touched components.
   - All unlogged/landing-page styles and components.
8. Produce no visual regression on the golden-path screenshot of
   the logged-in chat (confirmed by the reviewer via manual pass
   of the §6 manual verification checklist).

## 11. Related Specifications / Further Reading

- [spec-design-ux-refresh-navigation.md](./spec-design-ux-refresh-navigation.md)
  — Prior navigation UX work; this bugfix preserves its visual
  direction.
- Tailwind CSS v4 upgrade notes — "Buttons use the default cursor":
  https://tailwindcss.com/docs/upgrade-guide
- WAI-ARIA Authoring Practices — Keyboard Interaction for Button
  pattern: https://www.w3.org/WAI/ARIA/apg/patterns/button/
- MDN `:focus-visible` — behaviour and browser support:
  https://developer.mozilla.org/en-US/docs/Web/CSS/:focus-visible
- WCAG 2.1 Understanding SC 2.4.7 (Focus Visible):
  https://www.w3.org/WAI/WCAG21/Understanding/focus-visible
- WCAG 2.2 Understanding SC 1.4.11 (Non-text Contrast):
  https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast
