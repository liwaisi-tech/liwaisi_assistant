---
title: "Landing Page — Home Unlogged Experience"
version: 1.0
date_created: 2026-04-05
owner: Liwaisi Tech
tags: [design, frontend, backend, landing-page, marketing, B2B, waitlist]
---

# Introduction

This specification defines the design, content, and implementation of the Liwaisi Assistant **Landing Page** — the first experience for unauthenticated visitors. The page replaces the current minimal `LoginScreen.tsx` with a full, immersive, scroll-based landing page that communicates the product's value proposition, captures interested business emails via a waitlist, and provides a Google Sign-In entry point for existing users.

The landing page is the primary B2B sales surface for Liwaisi Assistant — an agentic AI platform powered by a Coloured Petri Net (CPN) engine designed for rural communities and agricultural businesses.

---

## 1. Purpose & Scope

### Purpose

Build an engaging, dark-themed, single-page landing experience that:

1. **Sells the vision**: Translates the CPN-based agentic OS into compelling business value for rural organizations
2. **Captures leads**: Collects Gmail/Google account emails via a waitlist form (B2B, no pricing)
3. **Authenticates existing users**: Preserves the Google Sign-In flow for returning users
4. **Tells the story**: Communicates Liwaisi's mission of technology as a seed of justice for rural communities

### Scope

- **In scope**: Frontend landing page (React 19), backend waitlist endpoint (Go), CSS animations, responsive layout, accessibility
- **Out of scope**: Pricing page, payment integration, CMS, analytics dashboard, internationalization (future)

### Intended Audience

- **Primary**: Rural business owners, agricultural cooperatives, NGOs working in rural development
- **Secondary**: Tech-savvy decision makers evaluating AI tools for rural operations
- **Tertiary**: Developers and engineers interested in CPN-based AI orchestration

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — a mathematical formalism for concurrent systems. In Liwaisi Assistant, CPN is the orchestration engine that coordinates AI agents, tools, and human input |
| **Place** | A CPN node that holds tokens (data). Visually represented as circles. Analogous to "stations" in a workflow |
| **Transition** | A CPN node that transforms tokens. Visually represented as rectangles. Analogous to "actions" or "steps" |
| **Token** | A colored data unit that flows through the CPN. Carries payloads, origin info, and color metadata |
| **ColorSet** | A type classification for tokens (e.g., ColorString, ColorHuman, ColorError). Determines which transitions can consume which tokens |
| **Space** | A token namespace — SpaceSurface (human-facing) vs SpaceComputation (AI-internal). Only HITL transitions cross spaces |
| **HITL** | Human-in-the-Loop — transitions that pause execution to wait for human approval, rejection, or revision |
| **Waitlist** | An email collection mechanism for interested businesses before the product is publicly available |
| **B2B** | Business-to-Business — the sales model; no consumer self-serve pricing |
| **SSE** | Server-Sent Events — the real-time streaming protocol used for CPN execution monitoring |
| **MCP** | Model Context Protocol — the standard for tool integration in the agent ecosystem |

---

## 3. Requirements, Constraints & Guidelines

### 3.1 Content Requirements (AI Marketing Product Expert)

- **REQ-MKT-001**: Hero section must communicate the core value proposition in one sentence: "AI workflows that think like a team, guided by your rules"
- **REQ-MKT-002**: All technical CPN concepts must be translated into agricultural/rural metaphors:
  - Places = "Estaciones" (stations where knowledge rests)
  - Transitions = "Procesos" (actions that transform)
  - Tokens = "Semillas" (seeds of data that flow and grow)
  - HITL = "Tu voz en cada paso" (your voice at every step)
- **REQ-MKT-003**: Copy must be bilingual-ready (Spanish primary, English secondary) but v1 is English only
- **REQ-MKT-004**: Mission section must include verbatim quotes from Liwaisi's founding principles
- **REQ-MKT-005**: CTA copy must emphasize exclusivity and B2B access: "Request Early Access" / "Join the Waitlist"
- **REQ-MKT-006**: No pricing, no "free trial" language — position as a premium B2B service with direct engagement

### 3.2 Design Requirements (UX/UI Designer)

- **REQ-DSN-001**: Dark theme using existing CSS custom properties:
  - `--bg-deep: #0a0a0f` (page background)
  - `--bg-surface: #12121a` (card surfaces)
  - `--bg-input: #1a1a26` (input fields)
  - `--accent: #0ea5e9` (primary accent — sky blue)
  - `--accent-glow: #0ea5e940` (glow effects)
  - `--text-primary: #e2e8f0` (headings, body)
  - `--text-secondary: #94a3b8` (descriptions)
  - `--text-muted: #64748b` (captions)
  - `--border-dim: #2a2a3a` (borders)
- **REQ-DSN-002**: Glass morphism surfaces (`.glass-surface` class) for feature cards
- **REQ-DSN-003**: Glow border effects (`.glow-border` class) for interactive elements
- **REQ-DSN-004**: Scan-line overlay (`.scan-lines` class) on hero section for terminal aesthetic
- **REQ-DSN-005**: Logo must be loaded from `/liwaisi_logo_dark_bg.svg` (copied to public assets)
- **REQ-DSN-006**: Typography: 'DM Sans' for body, 'JetBrains Mono' for headings and code elements
- **REQ-DSN-007**: Smooth scroll between sections with snap points
- **REQ-DSN-008**: Animated CPN visualization in hero section (CSS-only, no canvas/WebGL)
- **REQ-DSN-009**: Mobile-first responsive breakpoints: 320px, 640px, 768px, 1024px, 1280px
- **REQ-DSN-010**: Secondary accent color for nature/rural elements: `--accent-green: #10b981` (emerald)

### 3.3 Frontend Technical Requirements (AI Software Engineer)

- **REQ-FE-001**: Replace `LoginScreen.tsx` content with the new `LandingPage` component
- **REQ-FE-002**: No new npm dependencies — use existing React 19, Tailwind v4, CSS custom properties
- **REQ-FE-003**: Component structure:
  ```
  src/features/landing/
    LandingPage.tsx        — Main orchestrator
    HeroSection.tsx        — Hero with CPN animation + email CTA
    WhatIsSection.tsx      — Product explanation
    HowItWorksSection.tsx  — CPN flow visualization
    FeaturesSection.tsx    — Feature cards grid
    MissionSection.tsx     — Liwaisi values + social impact
    FooterCTA.tsx          — Final CTA + footer
    CPNAnimation.tsx       — Animated CPN diagram (CSS)
    WaitlistForm.tsx       — Email capture form component
  ```
- **REQ-FE-004**: `WaitlistForm` must validate email format (RFC 5322 basic) client-side
- **REQ-FE-005**: `WaitlistForm` must show loading, success, and error states
- **REQ-FE-006**: Google Sign-In button must remain available (via existing `AuthContext`)
- **REQ-FE-007**: All sections must use `IntersectionObserver` for scroll-triggered animations
- **REQ-FE-008**: Accessibility: all images have alt text, form has labels, color contrast >= 4.5:1, keyboard navigable
- **REQ-FE-009**: Landing page CSS additions go in `index.css` using existing custom property patterns
- **REQ-FE-010**: The `App.tsx` routing logic must be updated: when `!isAuthenticated`, render `LandingPage` instead of `LoginScreen`

### 3.4 CPN Visualization Requirements (CPN Mathematician Expert)

- **REQ-CPN-001**: Hero animation must show a simplified CPN with:
  - 3 Places (circles) representing: Input, Processing, Output
  - 2 Transitions (rectangles) representing: AI Agent, Human Review
  - Animated tokens (dots) flowing along arcs between places and transitions
  - Color coding: blue tokens (ColorString), green tokens (ColorHuman), amber tokens (ColorLLM)
- **REQ-CPN-002**: "How it Works" section must visualize a real-world CPN flow:
  - Place: "Your Request" → Transition: "AI Analysis" → Place: "Draft" → Transition: "Your Approval" → Place: "Final Result"
  - With an error arc: Transition: "Your Approval" → (reject) → Place: "Revision Queue" → Transition: "AI Revision" → Place: "Draft"
- **REQ-CPN-003**: Token animation must follow Petri net semantics:
  - Tokens consumed from input places before transition fires
  - Tokens produced in output places after transition completes
  - No phantom tokens (conservation property visualization)
- **REQ-CPN-004**: Visual distinction between SpaceSurface (human side, warm tones) and SpaceComputation (AI side, cool tones)
- **REQ-CPN-005**: HITL transition must be visually distinct — show a "pause" state with human icon

### 3.5 Backend Requirements (Waitlist Endpoint)

- **REQ-BE-001**: New endpoint `POST /api/v1/waitlist` accepting JSON body `{"email": "string"}`
- **REQ-BE-002**: Email validation: must be a valid email format, must not be empty
- **REQ-BE-003**: Duplicate email handling: return 200 OK (idempotent) — do not reveal if email already exists
- **REQ-BE-004**: Store in SQLite (existing DB) — new table `waitlist` with columns: `id`, `email`, `created_at`, `source`
- **REQ-BE-005**: Rate limiting: reuse existing `middleware_ratelimit.go` — 3 requests per minute per IP for waitlist endpoint
- **REQ-BE-006**: No authentication required for the waitlist endpoint
- **REQ-BE-007**: Response format: `{"ok": true, "message": "..."}` for success, `{"error": "..."}` for failure
- **REQ-BE-008**: CORS must allow the waitlist endpoint from the frontend origin

### 3.6 Security Constraints

- **SEC-001**: Email input must be sanitized against XSS and injection
- **SEC-002**: Rate limiting must prevent abuse of the waitlist endpoint
- **SEC-003**: No PII beyond email address is collected on the landing page
- **SEC-004**: Waitlist emails must not be exposed via any public API endpoint

### 3.7 Constraints

- **CON-001**: No new npm dependencies — zero bundle size increase from packages
- **CON-002**: Landing page must load under 3 seconds on 3G connection (< 200KB initial JS)
- **CON-003**: CPN animation must be CSS-only (no canvas, WebGL, or animation libraries)
- **CON-004**: Must work in Chrome 90+, Firefox 90+, Safari 15+, Edge 90+
- **CON-005**: Must not break existing authenticated user flow — LoginScreen replacement must be seamless

### 3.8 Guidelines

- **GUD-001**: Use semantic HTML5 elements (`<section>`, `<article>`, `<nav>`, `<footer>`)
- **GUD-002**: Prefer CSS animations over JS for scroll effects where possible
- **GUD-003**: Use `prefers-reduced-motion` media query to disable animations for accessibility
- **GUD-004**: Image assets should be lazy-loaded below the fold
- **GUD-005**: CPN visualization should progressively enhance — readable without animation

### 3.9 Patterns

- **PAT-001**: Section component pattern — each section is a self-contained component with its own scroll anchor ID
- **PAT-002**: CPN animation pattern — use CSS `@keyframes` with `animation-delay` for staggered token flow
- **PAT-003**: Form state pattern — use React `useState` for form states: `idle | loading | success | error`
- **PAT-004**: Intersection observer pattern — custom hook `useInView` for scroll-triggered animations

---

## 4. Interfaces & Data Contracts

### 4.1 Waitlist API Endpoint

```
POST /api/v1/waitlist
Content-Type: application/json

Request:
{
  "email": "business@example.com"
}

Response (201 Created):
{
  "ok": true,
  "message": "You're on the list! We'll reach out soon."
}

Response (400 Bad Request):
{
  "error": "invalid email format"
}

Response (429 Too Many Requests):
{
  "error": "too many requests, please try again later"
}
```

### 4.2 Waitlist Database Schema

```sql
CREATE TABLE IF NOT EXISTS waitlist (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    email      TEXT    NOT NULL UNIQUE,
    source     TEXT    NOT NULL DEFAULT 'landing_page',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_waitlist_email ON waitlist(email);
```

### 4.3 Frontend Component Props

```typescript
// WaitlistForm props
interface WaitlistFormProps {
  variant: 'hero' | 'footer';  // hero = larger, footer = compact
  className?: string;
}

// Section component pattern
interface SectionProps {
  id: string;
  className?: string;
  children: React.ReactNode;
}

// CPN Animation node types
interface CPNNode {
  type: 'place' | 'transition';
  label: string;
  x: number;
  y: number;
  color?: string;
}

interface CPNToken {
  color: string;  // CSS color
  path: number;   // Index of the arc path to follow
  delay: number;  // Animation delay in seconds
}
```

### 4.4 Landing Page Section IDs (for navigation anchors)

| Section | ID | Nav Label |
|---------|-----|-----------|
| Hero | `#hero` | (top) |
| What Is | `#what-is` | About |
| How It Works | `#how-it-works` | How It Works |
| Features | `#features` | Features |
| Mission | `#mission` | Our Mission |
| Footer CTA | `#get-access` | Get Access |

---

## 5. Acceptance Criteria

### Hero Section
- **AC-001**: Given the landing page loads, When the user sees the hero section, Then the Liwaisi logo is visible, the tagline is readable, and the email capture form is functional
- **AC-002**: Given the hero section, When the CPN animation plays, Then tokens visually flow between places and transitions with correct Petri net semantics
- **AC-003**: Given the hero section on mobile (< 640px), When rendered, Then the layout stacks vertically with the form below the tagline

### Waitlist Form
- **AC-004**: Given the waitlist form, When a user enters a valid email and submits, Then a POST request is sent to `/api/v1/waitlist` and a success message is shown
- **AC-005**: Given the waitlist form, When a user enters an invalid email, Then a client-side validation error is shown without making an API call
- **AC-006**: Given the waitlist form, When the API returns an error, Then an appropriate error message is displayed
- **AC-007**: Given the waitlist form is in loading state, When waiting for API response, Then the submit button shows a loading indicator and is disabled

### Navigation
- **AC-008**: Given the landing page, When the user scrolls down, Then a sticky navigation bar appears with section links
- **AC-009**: Given the navigation bar, When the user clicks a section link, Then the page smooth-scrolls to that section

### Google Sign-In
- **AC-010**: Given the landing page, When a returning user wants to sign in, Then the Google Sign-In button is accessible in the navigation area
- **AC-011**: Given a successful Google Sign-In, When the auth state changes, Then the user is redirected to the DesktopLayout (existing flow)

### Responsiveness
- **AC-012**: Given the landing page on desktop (>= 1024px), When rendered, Then feature cards display in a 3-column grid
- **AC-013**: Given the landing page on tablet (768px–1023px), When rendered, Then feature cards display in a 2-column grid
- **AC-014**: Given the landing page on mobile (< 768px), When rendered, Then all content stacks in a single column

### Backend
- **AC-015**: Given a valid email POST to `/api/v1/waitlist`, When the email is new, Then it is stored in the waitlist table and returns 201
- **AC-016**: Given a duplicate email POST to `/api/v1/waitlist`, When the email already exists, Then it returns 200 OK without error (idempotent)
- **AC-017**: Given more than 3 requests per minute from the same IP, When the 4th request arrives, Then it returns 429

### Accessibility
- **AC-018**: Given the landing page, When tested with a screen reader, Then all sections are navigable and content is announced correctly
- **AC-019**: Given the landing page, When tested with keyboard only, Then all interactive elements (form, buttons, links) are reachable via Tab

### Performance
- **AC-020**: Given the landing page, When loaded on a 3G connection, Then the First Contentful Paint is under 2 seconds and the page is interactive under 3 seconds

---

## 6. Test Automation Strategy

### Test Levels

- **Unit Tests**: WaitlistForm validation logic, email regex, API response handling
- **Integration Tests**: Waitlist endpoint (Go) — happy path, validation errors, duplicates, rate limiting
- **Visual Tests**: Screenshot comparison for key breakpoints (mobile, tablet, desktop)

### Frontend Testing

- Component render tests for each section (React Testing Library pattern)
- WaitlistForm: test all states (idle, loading, success, error)
- CPN animation: verify CSS animation classes are applied
- Intersection Observer: mock observer and verify class toggling

### Backend Testing

```go
// Waitlist handler tests
func TestHandleWaitlist_Success(t *testing.T)         // Valid email → 201
func TestHandleWaitlist_InvalidEmail(t *testing.T)     // Bad email → 400
func TestHandleWaitlist_EmptyEmail(t *testing.T)       // Empty → 400
func TestHandleWaitlist_DuplicateEmail(t *testing.T)   // Duplicate → 200
func TestHandleWaitlist_RateLimit(t *testing.T)        // Exceed limit → 429
```

### CI/CD Integration

- Frontend: `npm run build` must succeed with zero TypeScript errors
- Backend: `go test ./...` must pass including new waitlist handler tests
- Lighthouse CI: Performance score >= 90, Accessibility score >= 95

---

## 7. Rationale & Context

### Why a Landing Page Now?

The current `LoginScreen.tsx` is a bare Google Sign-In button with no context about what the product does. For B2B sales, potential clients need to understand the value proposition before committing to authentication. The landing page serves as the primary sales funnel.

### Why CPN Visualization?

Liwaisi Assistant's core differentiator is its CPN-based orchestration engine. While competitors use opaque "AI agent" marketing, Liwaisi can show a *mathematically formal, auditable* process. The CPN visualization makes this tangible — showing Places, Transitions, and Tokens creates trust through transparency.

### Why Agricultural Metaphors?

The CPN Mathematician Expert identified a natural mapping between Petri net concepts and agricultural workflows:
- **Seeds (Tokens)**: Data flows like seeds — planted, nurtured, harvested
- **Stations (Places)**: Workflow stages like field stations — each has a purpose
- **Processes (Transitions)**: Transformations like agricultural processes — milling, sorting, packaging
- **Human Voice (HITL)**: The farmer's expertise guides the system at critical decision points

This metaphor bridges the gap between formal computer science and the lived experience of rural communities.

### Why B2B with No Pricing?

The product is in early access. Pricing requires understanding each client's scale, needs, and context. The waitlist model allows for direct relationship-building, which aligns with Liwaisi's principle of "co-creation" — technology built with communities, not imposed on them.

### Dream Team Rationale

| Expert | Contribution |
|--------|-------------|
| AI Marketing Product Expert | Ensures copy sells the vision without jargon; translates CPN into business value |
| UX/UI Designer | Maintains the existing dark-space aesthetic while creating an engaging scroll experience |
| AI Software Engineer | Implements within existing constraints (no new deps, React 19, Tailwind v4) |
| CPN Mathematician Expert | Ensures the CPN visualization is mathematically accurate and pedagogically sound |

---

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Google Identity Services — Google Sign-In button rendering and OAuth2 token exchange (already integrated)

### Infrastructure Dependencies
- **INF-001**: SQLite database — existing persistence layer, extended with `waitlist` table
- **INF-002**: Vite dev server — serves the landing page during development
- **INF-003**: Static asset serving — logo SVG must be available at a public URL path

### Technology Platform Dependencies
- **PLT-001**: React 19 — existing frontend framework
- **PLT-002**: Tailwind CSS v4 — existing utility CSS framework
- **PLT-003**: Go 1.22+ — existing backend runtime
- **PLT-004**: Vite 6 — existing build tool

### Data Dependencies
- **DAT-001**: Liwaisi logo SVG — `resources/img/liwaisi_logo_dark_bg.svg` must be copied/linked to frontend public assets

---

## 9. Examples & Edge Cases

### CPN Animation — Token Flow Sequence

```
Frame 0:   [Place:Input ●●] → [Trans:AI] → [Place:Draft] → [Trans:HITL ⏸] → [Place:Output]
Frame 1:   [Place:Input ●]  → [Trans:AI ⚡] → [Place:Draft] → [Trans:HITL ⏸] → [Place:Output]
Frame 2:   [Place:Input ●]  → [Trans:AI] → [Place:Draft ●] → [Trans:HITL ⏸] → [Place:Output]
Frame 3:   [Place:Input ●]  → [Trans:AI] → [Place:Draft] → [Trans:HITL 👤●] → [Place:Output]
Frame 4:   [Place:Input ●]  → [Trans:AI] → [Place:Draft] → [Trans:HITL] → [Place:Output ●]
```

### Waitlist Form — Edge Cases

```typescript
// Valid emails
"user@gmail.com"           // standard Gmail
"business@company.co"      // business email
"name+tag@example.org"     // plus addressing

// Invalid emails (client-side rejection)
""                         // empty
"not-an-email"             // no @ symbol
"@domain.com"              // no local part
"user@"                    // no domain
"user @domain.com"         // spaces

// Backend edge cases
"USER@Gmail.COM"           // case-insensitive storage (lowercase before insert)
```

### Section Content — Marketing Copy Examples

```
Hero Tagline:
"AI that works like your best team — transparent, auditable, and always under your control."

Sub-tagline:
"Powered by Coloured Petri Nets. Built for rural innovation."

What Is Section:
"Liwaisi Assistant is an AI operating system where every decision is visible,
every workflow is auditable, and your voice guides every step.
Unlike black-box AI, our Petri Net engine shows you exactly
how data flows, when decisions are made, and why."

Feature Cards:
1. "Transparent Workflows" — "See every step. Every token, every transition, every decision — visible in real time."
2. "Human in the Loop" — "AI proposes, you decide. Approve, reject, or revise at any point."
3. "Agent Personality" — "Configure your AI's principles. Ethics first, always."
4. "Live Monitoring" — "Watch your workflows execute in real time with cost tracking."
5. "Tool Ecosystem" — "Connect any tool via MCP standard. Your AI, your toolbox."
6. "Flow Intelligence" — "Workflows learn and optimize. The more you use them, the better they get."
```

---

## 10. Validation Criteria

1. The landing page renders correctly at all responsive breakpoints (320px, 640px, 768px, 1024px, 1280px)
2. The CPN animation runs smoothly at 60fps with no layout shifts
3. The waitlist form successfully stores emails in the database
4. The Google Sign-In button works and transitions to the authenticated app
5. All text meets WCAG 2.1 AA contrast ratios (4.5:1 for normal text, 3:1 for large text)
6. The page scores >= 90 on Lighthouse Performance and >= 95 on Accessibility
7. The backend waitlist endpoint passes all integration tests (happy path, validation, duplicates, rate limiting)
8. The landing page loads in under 3 seconds on a simulated 3G connection
9. `prefers-reduced-motion` disables all CSS animations
10. No console errors or warnings in production build

---

## 11. Related Specifications / Further Reading

- [spec-architecture-google-oauth-login.md](spec-architecture-google-oauth-login.md) — Google OAuth integration details
- [spec-design-ux-refresh-navigation.md](spec-design-ux-refresh-navigation.md) — Design system and navigation patterns
- [spec-design-cpn-execution-visualizer.md](spec-design-cpn-execution-visualizer.md) — CPN visualization approach (for reuse in landing page)
- [spec-architecture-tools-engine-agent-personality.md](spec-architecture-tools-engine-agent-personality.md) — Agent personality system details
- [Liwaisi Tech Website](https://liwaisi.tech) — Brand information and mission
- [Liwaisi EdTech YouTube](https://www.youtube.com/@LiwaisiTech) — Educational content channel
