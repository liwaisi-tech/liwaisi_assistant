# brae OS — Design Handoff Roadmap

Source: `being-a-real-ai-enginer-brae/project/brae OS.html` (Claude Design export, 2026-04-21).

All six screens in the mock already exist in this codebase. This is a **re-theme**, not a feature-build. The work below is sequenced so you can ship value every step without blocking later ones.

---

## Foundation (done)

- `src/styles/brae-os.css` — additive design tokens, semantic palette (cyan / violet / amber / green / red), Inter Tight + Instrument Serif font imports, and `.brae-pill` / `.brae-rail` / `.brae-label-caps` / `.brae-dot` primitives. Wired in `main.tsx`.

Nothing existing changed yet — components opt in by using `--brae-*` vars / `.brae-*` classes.

---

## Screen-by-screen gap map

| Design screen | Existing feature | Gap vs. mock |
|---|---|---|
| `CHAT` | `features/chat` (`MessageList`, `MessageInput`, `ChatSidebar`, `a2ui/`, `hitl/`) | Rail accents on assistant/HITL bubbles, pipeline `<ol>` styling, A2UI FolderPicker/FileDrop chrome, composer pinned-context chips |
| `FLUJOS` | `features/cpn-visualizer/FlowBrowser` + `FlowDetail` | Card-grid with status pills, embedded CPN preview in detail view, KPI triplet (Invocaciones / Costo 7d / p50) |
| `MONITOR` | `features/execution-monitor/*` | Left execution list with per-item cost/time chips, floating cost badge on graph, violet "Nested" chip, 3-action toolbar (Reproducir / Comparar / Exportar) |
| `HERRAMIENTAS` | `features/tools/ToolBrowser` | Category nav sidebar with counts, 3-col card grid, HITL/Popular pill badges, search input with inline icon |
| `IDENTIDAD` | `features/personality/PersonalityPanel` + `HierarchyControl` + `PrincipleEditor` + `TensionVisualizer` | Drag-ordered priority stack with numbered chips, "Tensión con X" pill, Presets sidebar, versions list |
| `ADMIN` | `features/admin/AdminSecretsPanel` | Usage stat tiles, team-structure grid, HITL policies list, LLM-provider cost bars — this is the largest net-new surface |

---

## Prioritization (ship in this order)

### P0 — unblocks everything (≤ 1 day)

1. **Adopt tokens globally.** Alias `--accent` → `--brae-cyan`, introduce `--violet/--amber/--green/--red` at the non-namespaced level inside `index.css` so existing components inherit the brae palette without import churn. *Why first:* every later step depends on the palette.
2. **Swap the UI font** from DM Sans to Inter Tight in `body` (keep JetBrains Mono). Low-risk, immediately shifts the whole app toward the brae aesthetic.

### P1 — highest user-visible payoff per hour (≤ 3 days)

3. **Chat rail + HITL card treatment.** Wrap assistant bubbles in `.brae-rail-cyan`; wrap HITL cards in `.brae-rail-amber`/`-red` keyed on risk level. The mock's most distinctive visual motif.
4. **Composer pinned-context chips.** Small mono pills above the textarea for `plan-maestro.md`, `~/projects/brae`, etc. Data model already present in `MessageInput`.
5. **Topbar StatusChip + CostMeter.** The mock's gradient cost bar (`cyan → violet`) with budget ratio is a signature — replace or augment `StatusBar.tsx`.

### P2 — screen polish passes (1–2 days each, pick in any order)

6. **Tools screen** — left category nav + 3-col card grid. Currently in-flight per git status (`M front/react-assistant/src/features/tools/ToolBrowser.tsx`) — finish with the brae card shape (`brae-pill` for HITL/Popular, install/installed state).
7. **Flujos screen** — re-style `FlowBrowser` cards to match `FlowCard` from the mock (version pill, status dot, Invocaciones/Costo/p50 KPIs).
8. **Identidad screen** — drag-reorderable `PriorityStack` with the numbered circle + "Tensión con X" amber pill when a principle conflicts with a higher-priority layer. Your `TensionVisualizer.tsx` already computes conflicts; surface them as pills, not a separate panel.
9. **Monitor screen** — the floating cost badge + 3-action toolbar are low-effort high-signal upgrades to `ExecutionMonitor.tsx`.

### P3 — net-new surface area (not yet supported, schedule deliberately)

10. **Admin dashboard.** `AdminSecretsPanel` covers secrets only. The mock's Admin screen adds: (a) usage stat tiles (mes-spend, ejecuciones 24h, HITL pendientes, tools instalados), (b) team-structure grid, (c) HITL policies list with usage counts, (d) LLM-provider cost bars. Each of these needs a backend endpoint:
    - Spend + executions → already available via cost-ledger; wire into a new `AdminOverview.tsx`.
    - HITL policies → needs `GET /v1/policies` (likely already exists given `back/go-assistant/infra/host/policies/default.yaml`); exposing usage counts needs aggregation.
    - Team structure → out of scope today; the mock's teams are illustrative of a multi-tenant future. **Advice: stub as static content behind a flag, don't block on multi-tenant work.**
11. **A2UI FolderPicker / FileDrop chrome.** The inline `A2UI · FolderPicker` banner + path breadcrumb + file list is a distinct visual contract vs. your current `a2ui/` components. Worth aligning since this is the most innovative UX in the mock.

---

## Things the mock shows that you should *not* build yet

- **Drag-and-drop priority reordering** — today `HierarchyControl` sets order declaratively. The mock suggests drag handles (`Ic.drag`). Ship P2 #8 without DnD first; add `@dnd-kit` only if users ask.
- **"Nested" execution chip on monitor** — implies sub-CPN tracking. The existing `ExecutionList` doesn't distinguish nested executions; confirm with the backend before rendering a chip that lies.
- **Gradient cost bar** (`cyan → violet`) — pretty, but only meaningful if you have a budget. Gate behind whether the user has set `monthly_budget` in settings.

---

## Suggested first commit

1. Alias `--accent → var(--brae-cyan)` in `index.css`.
2. Switch `body { font-family }` to Inter Tight.
3. Wrap `MessageBubble` assistant variant with `className="brae-rail brae-rail-cyan"`.

That's three edits, zero behavior change, and the app immediately looks ~60% closer to the mock.
