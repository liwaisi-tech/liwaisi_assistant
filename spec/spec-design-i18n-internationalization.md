---
title: "Production-Level Internationalization (i18n) for React Frontend"
version: 1.0
date_created: 2026-04-05
last_updated: 2026-04-05
owner: liwaisi
tags: [design, frontend, i18n, internationalization, react, accessibility, localization]
---

# Introduction

This specification defines the implementation of production-level internationalization (i18n) for the Liwaisi Assistant React frontend at `front/react-assistant/`. The goal is to extract all hardcoded English strings from ~40+ components into a structured translation system supporting multiple languages, starting with English (en) and Spanish (es). The solution uses `react-i18next` as the industry-standard i18n framework for React, with namespace-based organization matching the existing feature directory structure, lazy loading of translation bundles, and a user-facing language switcher.

## 1. Purpose & Scope

### Purpose

Add internationalization support to the Liwaisi Assistant frontend to:

1. Enable the application to serve users in multiple languages (initially English and Spanish)
2. Extract all ~200+ hardcoded English strings into structured, maintainable translation files
3. Provide a seamless language switching experience with persisted preferences
4. Support future expansion to additional languages (Portuguese, French, etc.) and RTL scripts (Arabic, Hebrew)
5. Maintain the existing dark glassmorphism design system and UX patterns

### Scope

- **In scope**: i18n framework setup, translation file structure, string extraction from all components, language switcher UI, localStorage persistence, HTML `lang` attribute management, TypeScript type safety, test infrastructure, date/number formatting, pluralization
- **Out of scope**: Server-side rendering (SSR), URL-based locale routing (no router exists), backend API i18n, translation of user-generated content (chat messages), translation of CPN topology names/identifiers, machine translation integration, content management system (CMS) for translations

### Intended Audience

- Frontend developers implementing the i18n system
- AI code generation agents building from this specification
- Translators creating/maintaining translation files

### Assumptions

- React 19, Tailwind CSS v4, Vite 6, TypeScript 5.7 remain the technology stack
- State-based navigation continues (no URL router)
- All existing hooks and components remain functionally unchanged
- The design system (CSS custom properties, glassmorphism, dark theme) is preserved
- English is the default/fallback language
- Spanish translations will be provided for all strings at launch
- Backend API responses remain in English (API i18n is a future concern)

## 2. Definitions

| Term | Definition |
|------|-----------|
| **i18n** | Internationalization — the process of designing software so it can be adapted to various languages and regions without engineering changes. Abbreviated as i18n (i + 18 letters + n). |
| **l10n** | Localization — the process of adapting internationalized software for a specific region or language by translating text and adjusting formats. |
| **Namespace** | A logical grouping of translation keys corresponding to a feature area (e.g., `landing`, `chat`, `desktop`). Each namespace maps to a separate JSON file, enabling lazy loading. |
| **Translation Key** | A dot-separated string identifier used to look up translated text (e.g., `landing:hero.title`). The part before `:` is the namespace, after is the key path. |
| **Interpolation** | Inserting dynamic values into translated strings using `{{variable}}` syntax (e.g., `"Hello, {{name}}"` renders as `"Hello, Juan"`). |
| **Pluralization** | Selecting the correct translation variant based on a count value. i18next uses `_one`, `_other` suffixes (e.g., `message_one: "1 message"`, `message_other: "{{count}} messages"`). |
| **Fallback Language** | The language used when a translation key is missing in the active language. English (`en`) is the fallback. |
| **RTL** | Right-to-Left — text direction used by Arabic, Hebrew, and other scripts. The i18n architecture must be RTL-ready without requiring immediate RTL implementation. |
| **Language Detector** | A plugin that automatically determines the user's preferred language from browser settings, localStorage, or URL parameters. |
| **Trans Component** | A React component from `react-i18next` that enables translations containing JSX elements (e.g., bold text, links within a sentence). |
| **CPN** | Coloured Petri Net — the execution model used by the backend AI agent engine. |
| **SSE** | Server-Sent Events — real-time streaming protocol for messages and execution events. |
| **HITL** | Human-In-The-Loop — approval/rejection interaction pattern within chat messages. |

## 3. Requirements, Constraints & Guidelines

### Framework & Libraries

- **REQ-001**: Use `i18next` as the core internationalization framework.
- **REQ-002**: Use `react-i18next` for React integration, providing `useTranslation` hook, `Trans` component, and `I18nextProvider`.
- **REQ-003**: Use `i18next-browser-languagedetector` for automatic language detection from browser preferences and localStorage.
- **REQ-004**: Translation files MUST be static JSON imports (not HTTP-fetched) to avoid runtime network overhead. Use dynamic `import()` for lazy loading per namespace.

### Language Support

- **REQ-005**: Support English (`en`) as the default and fallback language.
- **REQ-006**: Support Spanish (`es`) as the second language at launch.
- **REQ-007**: The architecture MUST support adding new languages by creating a new locale directory and JSON files without code changes.
- **REQ-008**: Missing translation keys MUST fall back to English, never display raw keys to users.

### Translation File Organization

- **REQ-009**: Translation files MUST be organized by namespace matching the feature structure:
  ```
  src/i18n/
  ├── config.ts                     # i18next initialization
  ├── types.ts                      # TypeScript types for keys
  └── locales/
      ├── en/
      │   ├── common.json           # Shared: buttons, labels, errors, status
      │   ├── landing.json          # Landing page: hero, features, mission, footer
      │   ├── auth.json             # Authentication: login, sign-in modal
      │   ├── chat.json             # Chat: messages, input, sidebar, dialogs
      │   ├── desktop.json          # Desktop: navigation, command palette, status bar
      │   ├── monitor.json          # Execution monitor: metrics, timeline, inspector
      │   ├── flows.json            # CPN visualizer: flow browser, topology
      │   ├── personality.json      # Agent identity: principles, hierarchy
      │   └── tools.json            # Tool browser
      └── es/
          ├── common.json
          ├── landing.json
          ├── auth.json
          ├── chat.json
          ├── desktop.json
          ├── monitor.json
          ├── flows.json
          ├── personality.json
          └── tools.json
  ```
- **REQ-010**: Translation keys MUST use dot-separated camelCase paths (e.g., `hero.title`, `features.cpnEngine.title`).
- **REQ-011**: The `common` namespace MUST contain shared strings used across multiple features: button labels (`save`, `cancel`, `delete`, `retry`), status labels (`connected`, `disconnected`, `loading`, `error`), and generic UI text.

### String Extraction

- **REQ-012**: ALL user-facing hardcoded strings MUST be extracted from components into translation files. This includes:
  - Visible text content
  - Placeholder text (input fields)
  - `aria-label` and `aria-describedby` values
  - `title` attributes
  - Error messages
  - Toast/notification messages
  - Alt text for images
- **REQ-013**: Dynamic strings with variables MUST use i18next interpolation syntax: `t('chat:messages.copied', { count: 5 })`.
- **REQ-014**: Strings containing JSX elements (bold, links, etc.) MUST use the `Trans` component from `react-i18next`.
- **REQ-015**: Strings that are purely technical identifiers (CSS classes, data attributes, API endpoints, CPN topology IDs) MUST NOT be extracted.

### Language Switcher UI

- **REQ-016**: A language switcher MUST appear in the landing page navigation bar, positioned before the "Get Access" / sign-in button.
- **REQ-017**: A language switcher MUST appear in the desktop layout's `StatusBar` component, positioned in the right section near the connection indicator.
- **REQ-018**: The language switcher MUST display the current language using its native name abbreviation (e.g., "EN", "ES") with a globe icon.
- **REQ-019**: Clicking the switcher MUST open a dropdown listing available languages by their native names (e.g., "English", "Espanol").
- **REQ-020**: Language switching MUST be instant — no page reload required.
- **REQ-021**: The switcher dropdown MUST close on outside click or Escape key press.

### Language Persistence & Detection

- **REQ-022**: The selected language MUST be persisted in `localStorage` under the key `liwaisi_lang`.
- **REQ-023**: On first visit, language MUST be detected in this priority order:
  1. `localStorage` value (`liwaisi_lang`)
  2. Browser `navigator.language` / `navigator.languages`
  3. Default to `en`
- **REQ-024**: The HTML `<html>` element's `lang` attribute MUST be updated whenever the language changes.
- **REQ-025**: The HTML `<html>` element's `dir` attribute MUST be set to `ltr` for LTR languages and `rtl` for RTL languages (future-proofing).

### Date, Number & Formatting

- **REQ-026**: Use the `Intl.DateTimeFormat` API for locale-aware date formatting (e.g., time-ago strings in chat sidebar).
- **REQ-027**: Use the `Intl.NumberFormat` API for locale-aware number and currency formatting (e.g., cost display `$0.05`).
- **REQ-028**: Relative time formatting (e.g., "just now", "5m ago", "2h ago") MUST be translated and use `Intl.RelativeTimeFormat` where supported.

### Pluralization

- **REQ-029**: All countable strings MUST support pluralization using i18next plural suffixes (`_one`, `_other` for English; `_one`, `_many`, `_other` for Spanish where applicable).
- **REQ-030**: Example: `message_one: "{{count}} message will be copied"`, `message_other: "{{count}} messages will be copied"`.

### TypeScript Type Safety

- **REQ-031**: Translation keys MUST be type-safe. Create a TypeScript declaration that types the `t()` function return based on the English translation files as the source of truth.
- **REQ-032**: The type definition MUST be generated or structured so that invalid translation keys produce compile-time errors.
- **REQ-033**: Use `react-i18next`'s built-in module augmentation pattern via `react-i18next.d.ts` to declare custom type options referencing the English resource files.

### Performance

- **CON-001**: The i18n runtime bundle (i18next + react-i18next + detector) MUST add less than 15KB gzipped to the total bundle.
- **CON-002**: Translation files MUST be loaded lazily by namespace. Only the `common` namespace and the active view's namespace should load on initial page render.
- **CON-003**: Language switching MUST NOT trigger a full page reload or re-mount the React tree. Only translated text should re-render.
- **CON-004**: Translation lookups via `t()` MUST be synchronous after initial load (no async rendering or suspense boundaries for translations).

### RTL Readiness

- **GUD-001**: Use logical CSS properties (`margin-inline-start` instead of `margin-left`, `padding-inline-end` instead of `padding-right`) in new i18n-related components (language switcher). Existing components do NOT need RTL conversion in this iteration.
- **GUD-002**: The i18n configuration MUST include a language-to-direction mapping so that adding an RTL language in the future only requires adding the mapping entry and translation files.

### Accessibility

- **REQ-034**: The language switcher MUST be fully keyboard-accessible (Tab to focus, Enter/Space to open, Arrow keys to navigate options, Escape to close).
- **REQ-035**: The language switcher MUST use appropriate ARIA attributes (`role="listbox"`, `aria-expanded`, `aria-selected`, `aria-label`).
- **REQ-036**: Screen readers MUST announce language changes via an `aria-live="polite"` region.

### Code Patterns

- **PAT-001**: In functional components, use the `useTranslation` hook with explicit namespace:
  ```tsx
  const { t } = useTranslation('chat');
  return <h1>{t('header.title')}</h1>;
  ```
- **PAT-002**: For components using multiple namespaces:
  ```tsx
  const { t } = useTranslation(['chat', 'common']);
  return <button>{t('common:buttons.cancel')}</button>;
  ```
- **PAT-003**: For strings with embedded JSX:
  ```tsx
  <Trans i18nKey="landing:hero.subtitle" components={{ bold: <strong /> }} />
  ```
- **PAT-004**: For pluralization:
  ```tsx
  t('chat:fork.messageCount', { count: selectedMessages.length })
  ```
- **PAT-005**: Create a reusable `LanguageSwitcher` component in `src/features/i18n/LanguageSwitcher.tsx` used by both landing and desktop layouts.

## 4. Interfaces & Data Contracts

### i18n Configuration Interface

```typescript
// src/i18n/config.ts
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

// Language metadata for the switcher and direction management
export interface LanguageMeta {
  code: string;         // ISO 639-1 code: 'en', 'es'
  name: string;         // Native name: 'English', 'Español'
  dir: 'ltr' | 'rtl';  // Text direction
}

export const SUPPORTED_LANGUAGES: LanguageMeta[] = [
  { code: 'en', name: 'English', dir: 'ltr' },
  { code: 'es', name: 'Español', dir: 'ltr' },
];

export const DEFAULT_LANGUAGE = 'en';
export const LANGUAGE_STORAGE_KEY = 'liwaisi_lang';

// Namespace definitions matching feature directories
export const NAMESPACES = [
  'common',
  'landing',
  'auth',
  'chat',
  'desktop',
  'monitor',
  'flows',
  'personality',
  'tools',
] as const;

export type Namespace = typeof NAMESPACES[number];
```

### Translation File Schema (English Source of Truth)

```jsonc
// src/i18n/locales/en/common.json
{
  "buttons": {
    "cancel": "Cancel",
    "save": "Save",
    "delete": "Delete",
    "retry": "Retry",
    "close": "Close",
    "send": "Send",
    "reset": "Reset",
    "clear": "Clear",
    "fork": "Fork",
    "rename": "Rename",
    "signOut": "Sign out"
  },
  "status": {
    "connected": "Connected",
    "disconnected": "Disconnected",
    "loading": "Loading...",
    "idle": "Idle",
    "running": "Running",
    "waiting": "Waiting",
    "completed": "Completed",
    "failed": "Failed"
  },
  "errors": {
    "generic": "Something went wrong. Please try again.",
    "tooManyRequests": "Too many requests. Please try again in a moment.",
    "invalidEmail": "Please enter a valid email address"
  },
  "time": {
    "justNow": "just now",
    "minutesAgo": "{{count}}m ago",
    "hoursAgo": "{{count}}h ago",
    "yesterday": "Yesterday",
    "daysAgo": "{{count}}d ago",
    "weeksAgo": "{{count}}w ago",
    "monthsAgo": "{{count}}mo ago",
    "yearsAgo": "{{count}}y ago"
  },
  "a11y": {
    "languageSwitcher": "Change language",
    "currentLanguage": "Current language: {{language}}"
  }
}
```

```jsonc
// src/i18n/locales/en/landing.json
{
  "nav": {
    "about": "About",
    "howItWorks": "How It Works",
    "features": "Features",
    "ourMission": "Our Mission",
    "getAccess": "Get Access"
  },
  "hero": {
    "title": "AI that works like your best team",
    "subtitle": "Transparent, auditable, and always under your control. Powered by Coloured Petri Nets. Built for rural innovation.",
    "cta": "Join our early access program. B2B only — we'll reach out personally."
  },
  "whatIs": {
    "title": "What is Liwaisi Assistant?",
    "subtitle": "An AI operating system where every decision is visible",
    "description": "Unlike black-box AI, Liwaisi Assistant uses a Coloured Petri Net engine to orchestrate AI workflows with mathematical precision. Every token of data, every transition, every decision — visible and auditable in real time. Your team stays in control at every step.",
    "transparent": { "title": "Transparent", "description": "See every step of every workflow. No hidden prompts, no mystery reasoning." },
    "auditable": { "title": "Auditable", "description": "Every execution is recorded. Full cost tracking, token usage, and decision history." },
    "humanFirst": { "title": "Human-First", "description": "AI proposes, you decide. Approve, reject, or revise at any critical point." }
  },
  "howItWorks": {
    "title": "How it works",
    "subtitle": "From request to result, every step visible",
    "step1": {
      "label": "You make a request",
      "tag": "Place: Surface Space",
      "description": "Your input enters the system as a colored token in the Surface space — the human-facing boundary where you maintain full control."
    },
    "step2": {
      "label": "AI agents analyze and draft",
      "tag": "Transition: Computation Space",
      "description": "LLM transitions fire in the Computation space — consuming your input token, calling AI models, executing tools, and producing a structured draft."
    },
    "step3": {
      "label": "You review and decide",
      "tag": "HITL Transition: Space Bridge",
      "description": "The Human-in-the-Loop transition pauses the net and presents the draft. You approve, reject, or request revisions — your voice crosses from Surface to Computation."
    },
    "step4": {
      "label": "Result delivered",
      "tag": "Place: Output",
      "description": "The approved result lands in the Output place — auditable, cost-tracked, and ready. Every step recorded for future optimization."
    }
  },
  "features": {
    "title": "Capabilities",
    "subtitle": "Built for serious AI workflows",
    "cpnEngine": { "title": "Agentic CPN Engine", "description": "Coloured Petri Net orchestration provides deterministic, concurrent AI workflows. Every execution follows a mathematically verified topology." },
    "hitl": { "title": "Human in the Loop", "description": "Approval and revision loops at critical decision points. AI proposes, humans decide — with multi-round revision when needed." },
    "personality": { "title": "Agent Personality", "description": "Configure your AI's principles: nucleo, conducta, etica. Define tension rules and hierarchies that shape every response." },
    "monitoring": { "title": "Live Monitoring", "description": "Real-time SSE-powered execution visualization. Watch tokens flow, transitions fire, and costs accumulate — live." },
    "tools": { "title": "Tool Ecosystem", "description": "MCP-standard tool integration. Connect databases, APIs, file systems, and custom tools into your AI workflows." },
    "flowIntelligence": { "title": "Flow Intelligence", "description": "Topologies crystallize, rank, and optimize over time. The more you use a workflow, the better it gets — measured by real execution data." }
  },
  "mission": {
    "title": "Our Mission",
    "subtitle": "Technology as a seed of justice",
    "description": "We bring technology to the field as a tool to sow opportunities, strengthen community knowledge, and care for the land while growing together. We create and teach technology made with communities, so that rural people can live well, pursue their ideas, and defend their right to stay on their land with pride, dignity, and progress.",
    "quote": "We imagine a territory where learning technology is as common as farming, and where our communities are leaders of a new rurality — prosperous, creative, and just.",
    "quoteAttribution": "— Liwaisi 10-Year Vision",
    "principles": {
      "humanFirst": { "title": "Human First", "description": "Technology at the service of people, not the other way around." },
      "territorial": { "title": "Territorial Knowledge", "description": "We value traditional wisdom and build from it, not over it." },
      "impact": { "title": "Positive Impact", "description": "Tech solutions that improve lives, care for the planet, and generate income." },
      "coCreation": { "title": "Co-creation", "description": "Everything built in dialogue with people and communities." },
      "sovereignty": { "title": "Tech Sovereignty", "description": "Supporting autonomy to cultivate and to create your own technology." },
      "justice": { "title": "Social Justice", "description": "Businesses that reduce inequalities, not deepen them." }
    }
  },
  "footer": {
    "earlyAccess": "Early Access",
    "readyTitle": "Ready to see AI you can trust?",
    "readyDescription": "We work directly with each partner to build AI workflows tailored to your needs. No black boxes. No surprises.",
    "alreadyHaveAccess": "Already have access?",
    "signIn": "Sign in",
    "stats": {
      "cpn": "CPN",
      "cpnLabel": "Petri Net Engine",
      "blocks": "6+",
      "blocksLabel": "Building Blocks",
      "hitl": "HITL",
      "hitlLabel": "Human-in-the-Loop",
      "b2b": "B2B",
      "b2bLabel": "Direct Partnership"
    },
    "columns": {
      "product": "Product",
      "community": "Community",
      "resources": "Resources"
    },
    "companyName": "Liwaisi Tech",
    "companyTagline": "Technology as a seed of justice, empowerment, and abundance for rural communities. Built with communities, for communities.",
    "communityLinks": {
      "edtech": "EdTech Workshops",
      "tutorials": "Video Tutorials",
      "development": "Software Development"
    },
    "youtube": "Liwaisi Tech YouTube channel",
    "copyright": "{{year}} Liwaisi Tech. Technology for rural justice.",
    "builtWith": "Built with Coloured Petri Nets"
  },
  "waitlist": {
    "emailLabel": "Email address",
    "emailPlaceholder": "your@email.com",
    "submit": "Request Early Access",
    "submitting": "Joining...",
    "success": "You're on the list! We'll reach out soon."
  },
  "signInModal": {
    "closeLabel": "Close sign-in dialog",
    "title": "Sign in to Liwaisi Assistant",
    "welcomeBack": "Welcome back",
    "signInPrompt": "Sign in to your command center",
    "or": "or",
    "joinWaitlist": "Join Waitlist Instead"
  }
}
```

```jsonc
// src/i18n/locales/en/chat.json
{
  "header": {
    "title": "Liwaisi Assistant",
    "newConversation": "New conversation"
  },
  "roles": {
    "user": "You",
    "assistant": "AI"
  },
  "input": {
    "placeholderIdle": "Type a message...",
    "placeholderWaiting": "Review the plan above...",
    "placeholderRunning": "Waiting for response...",
    "ariaLabel": "Message input",
    "sendLabel": "Send message",
    "helperText": "Press Enter to send, Shift+Enter for newline"
  },
  "sidebar": {
    "costFormat": "${{amount}}",
    "costMinimal": "<$0.01"
  },
  "confirmClear": {
    "title": "Start new conversation?",
    "description": "This will clear the current conversation. This action cannot be undone."
  },
  "fork": {
    "title": "Fork Conversation",
    "description": "Select the fork point. Messages up to this point will be copied.",
    "messageCount_one": "{{count}} message will be copied. Cost resets to $0.00",
    "messageCount_other": "{{count}} messages will be copied. Cost resets to $0.00"
  }
}
```

```jsonc
// src/i18n/locales/en/desktop.json
{
  "nav": {
    "chat": "Chat",
    "flows": "Flows",
    "monitor": "Monitor",
    "personality": "Agent Identity",
    "tools": "Tool Browser"
  },
  "shortcuts": {
    "chat": "⌘1",
    "flows": "⌘2",
    "monitor": "⌘3",
    "personality": "⌘4",
    "tools": "⌘5"
  },
  "commandPalette": {
    "categories": {
      "navigation": "Navigation",
      "chat": "Chat",
      "view": "View"
    },
    "actions": {
      "goToChat": "Go to Chat",
      "goToFlows": "Go to Flows",
      "goToMonitor": "Go to Monitor",
      "agentIdentity": "Agent Identity",
      "toolBrowser": "Tool Browser",
      "newChat": "New Chat",
      "toggleSidebar": "Toggle Chat Sidebar",
      "openFlowsPanel": "Open Flows Panel",
      "openMonitorPanel": "Open Monitor Panel"
    }
  }
}
```

```jsonc
// src/i18n/locales/en/monitor.json
{
  "title": "Monitor",
  "metrics": {
    "transitionsFired": "Transitions fired",
    "llmCalls": "LLM calls",
    "totalCost": "Total cost",
    "eventCount": "Event count"
  },
  "executionList": {
    "title": "Executions"
  },
  "transitionInspector": {
    "title": "Transition Inspector"
  },
  "timeline": {
    "title": "Timeline"
  }
}
```

```jsonc
// src/i18n/locales/en/personality.json
{
  "title": "Agent Identity",
  "version": "v{{version}} · {{date}}",
  "resetButton": "Reset",
  "resetConfirm": "Reset to defaults?",
  "loading": "Loading agent identity...",
  "kinds": {
    "nucleo": "nucleo",
    "conducta": "conducta",
    "etica": "etica"
  }
}
```

### TypeScript Type Declaration

```typescript
// src/i18n/types.ts — react-i18next module augmentation
import 'react-i18next';
import type common from './locales/en/common.json';
import type landing from './locales/en/landing.json';
import type auth from './locales/en/auth.json';
import type chat from './locales/en/chat.json';
import type desktop from './locales/en/desktop.json';
import type monitor from './locales/en/monitor.json';
import type flows from './locales/en/flows.json';
import type personality from './locales/en/personality.json';
import type tools from './locales/en/tools.json';

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common';
    resources: {
      common: typeof common;
      landing: typeof landing;
      auth: typeof auth;
      chat: typeof chat;
      desktop: typeof desktop;
      monitor: typeof monitor;
      flows: typeof flows;
      personality: typeof personality;
      tools: typeof tools;
    };
  }
}
```

### LanguageSwitcher Component Interface

```typescript
// src/features/i18n/LanguageSwitcher.tsx
interface LanguageSwitcherProps {
  /** Visual variant matching the context (landing page vs desktop app) */
  variant: 'landing' | 'desktop';
  /** Optional additional CSS classes */
  className?: string;
}
```

### Language Change Side Effect

```typescript
// Called on every language change (via i18next 'languageChanged' event)
function onLanguageChanged(lang: string): void {
  // 1. Update <html lang="...">
  document.documentElement.lang = lang;
  // 2. Update <html dir="...">
  const meta = SUPPORTED_LANGUAGES.find(l => l.code === lang);
  document.documentElement.dir = meta?.dir ?? 'ltr';
  // 3. Persist to localStorage
  localStorage.setItem(LANGUAGE_STORAGE_KEY, lang);
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a user visits the app for the first time with browser language set to `es`, When the app loads, Then all UI text renders in Spanish and `<html lang="es">` is set.
- **AC-002**: Given a user visits the app for the first time with browser language set to `fr` (unsupported), When the app loads, Then all UI text renders in English (fallback) and `<html lang="en">` is set.
- **AC-003**: Given a user is on the landing page, When they click the language switcher and select "Espanol", Then all landing page text immediately switches to Spanish without a page reload.
- **AC-004**: Given a user selected Spanish, When they close and reopen the browser, Then the app loads in Spanish (persisted in `localStorage` under `liwaisi_lang`).
- **AC-005**: Given a user is authenticated and in the desktop layout, When they click the language switcher in the StatusBar and select "English", Then all desktop UI text switches to English instantly.
- **AC-006**: Given the fork dialog shows "5 messages will be copied", When the language is Spanish, Then it displays "5 mensajes seran copiados" (correct pluralization).
- **AC-007**: Given a translation key exists in English but is missing in the Spanish translation file, When the app renders in Spanish, Then the English fallback text is displayed (never raw keys).
- **AC-008**: Given the `useTranslation` hook is used with an invalid key in TypeScript, When the developer compiles the project, Then TypeScript reports a type error.
- **AC-009**: Given the app loads in English, When only the `common` and `landing` namespaces are needed, Then only those two JSON files are loaded (verifiable via browser DevTools or build output).
- **AC-010**: Given the language is changed from English to Spanish, When a screen reader is active, Then the language change is announced via an `aria-live` region.
- **AC-011**: Given existing tests in `ChatSidebar.test.tsx` and `MessageBubble.test.tsx`, When the i18n system is integrated, Then all existing tests pass with the i18n test wrapper providing English translations.
- **AC-012**: Given the chat sidebar shows relative times, When the language is Spanish, Then time-ago strings render in Spanish (e.g., "hace 5m", "Ayer").
- **AC-013**: Given the i18n bundle is added, When measuring the production build, Then the total gzipped size increase is under 15KB for the i18n runtime.

## 6. Test Automation Strategy

### Test Levels

| Level | Scope | Framework |
|-------|-------|-----------|
| **Unit** | i18n config initialization, language detection, namespace loading, formatting utilities | Vitest |
| **Component** | LanguageSwitcher rendering, translation rendering in components, locale switching | Vitest + @testing-library/react |
| **Integration** | Full app rendering with i18n provider, language persistence across components | Vitest + @testing-library/react |

### Test Infrastructure

**i18n Test Helper** — Create a reusable test wrapper at `src/test/i18n-test-utils.tsx`:

```typescript
import { I18nextProvider } from 'react-i18next';
import i18n from '../i18n/config';

// Initialize i18n for tests with all English translations loaded synchronously
export function createTestI18n(language = 'en') {
  const testI18n = i18n.cloneInstance();
  testI18n.changeLanguage(language);
  return testI18n;
}

export function I18nTestWrapper({ children }: { children: React.ReactNode }) {
  return <I18nextProvider i18n={createTestI18n()}>{children}</I18nextProvider>;
}
```

### Test Cases

**i18n Configuration Tests** (`src/i18n/__tests__/config.test.ts`):
- Initializes with English as default language
- Falls back to English for unsupported languages
- Loads all namespaces without errors
- Updates `document.documentElement.lang` on language change
- Updates `document.documentElement.dir` on language change
- Persists language to `localStorage`
- Reads language from `localStorage` on init

**LanguageSwitcher Tests** (`src/features/i18n/__tests__/LanguageSwitcher.test.tsx`):
- Renders current language abbreviation
- Opens dropdown on click
- Lists all supported languages
- Changes language on selection
- Closes dropdown on outside click
- Closes dropdown on Escape key
- Is keyboard accessible (Tab, Enter, Arrow keys)
- Has correct ARIA attributes
- Renders `landing` variant styling
- Renders `desktop` variant styling

**Translation Completeness Test** (`src/i18n/__tests__/completeness.test.ts`):
- All namespaces in `es` have the same keys as `en`
- No translation value is an empty string
- All interpolation variables in `en` exist in `es`
- All plural forms required by the language are present

**Existing Test Compatibility**:
- Wrap existing test renders with `I18nTestWrapper`
- Verify all existing assertions still pass
- Update any string-matching assertions to use translation keys or translated values

### Coverage Requirements

- **Minimum 85%** line coverage for all files in `src/i18n/`
- **Minimum 85%** line coverage for `src/features/i18n/LanguageSwitcher.tsx`
- Existing component tests must maintain their current coverage levels

### CI/CD Integration

- All i18n tests run as part of `npm test` / `vitest`
- Translation completeness test catches missing keys before merge
- TypeScript compilation catches invalid translation key usage

## 7. Rationale & Context

### Why react-i18next?

`react-i18next` is the de facto standard for React internationalization with 5M+ weekly npm downloads. It provides:
- Deep React integration (hooks, components, HOC patterns)
- Namespace-based lazy loading matching our feature structure
- Built-in pluralization following CLDR rules
- TypeScript support via module augmentation
- Active maintenance and React 19 compatibility
- Lightweight runtime (~8KB gzipped for i18next + react-i18next combined)

### Why static imports over HTTP backend?

The `i18next-http-backend` plugin loads translations via HTTP at runtime, which adds network latency and failure modes. Since this is a SPA bundled with Vite, static JSON imports via dynamic `import()` provide:
- Zero network overhead (translations bundled in JS chunks)
- Guaranteed availability (no fetch failures)
- Vite code-splitting for lazy loading per namespace
- Simpler deployment (no separate translation API or CDN)

### Why namespace-per-feature?

Matching i18n namespaces to the existing `features/` directory structure:
- Makes it obvious where to find/add translations for a given component
- Enables lazy loading — the landing page doesn't need chat translations
- Scales naturally as new features are added
- Prevents a single monolithic translation file from growing unwieldy

### Why Spanish as the second language?

Liwaisi is a Colombian rural technology organization. Spanish is the primary language of their user base. English is maintained as the development language and for international visibility. The mission statement and landing page content specifically reference rural Colombian communities.

### Why RTL-readiness without RTL implementation?

Adding RTL support after the fact is expensive (requires refactoring CSS across all components). Including the `dir` attribute management and language-to-direction mapping now costs nearly nothing and prevents architectural debt.

## 8. Dependencies & External Integrations

### Technology Platform Dependencies

- **PLT-001**: `i18next` (^24.0.0) — Core i18n framework providing translation loading, interpolation, pluralization, and namespace management. Must support React 19.
- **PLT-002**: `react-i18next` (^15.0.0) — React bindings providing `useTranslation` hook, `Trans` component, `I18nextProvider`, and TypeScript module augmentation.
- **PLT-003**: `i18next-browser-languagedetector` (^8.0.0) — Browser language detection plugin reading from localStorage, navigator, and HTML tag.

### Infrastructure Dependencies

- **INF-001**: Vite dynamic imports — Used for lazy loading translation JSON files per namespace. Relies on Vite's code-splitting behavior.
- **INF-002**: Browser `Intl` API — Used for locale-aware date, number, and relative time formatting. Supported in all modern browsers (Chrome 24+, Firefox 29+, Safari 10+).

### Data Dependencies

- **DAT-001**: Translation JSON files — English files are the source of truth. Spanish files must maintain key parity with English files. Future languages follow the same contract.

## 9. Examples & Edge Cases

### Example: Basic Component Translation

**Before (hardcoded):**
```tsx
// ChatHeader.tsx
export function ChatHeader() {
  return <h1>Liwaisi Assistant</h1>;
}
```

**After (internationalized):**
```tsx
// ChatHeader.tsx
import { useTranslation } from 'react-i18next';

export function ChatHeader() {
  const { t } = useTranslation('chat');
  return <h1>{t('header.title')}</h1>;
}
```

### Example: Pluralization

**Translation file:**
```json
{
  "fork": {
    "messageCount_one": "{{count}} message will be copied. Cost resets to $0.00",
    "messageCount_other": "{{count}} messages will be copied. Cost resets to $0.00"
  }
}
```

**Component:**
```tsx
<p>{t('fork.messageCount', { count: selectedMessages.length })}</p>
```

### Example: Interpolation with Currency

**Translation file:**
```json
{
  "sidebar": {
    "costFormat": "${{amount}}"
  }
}
```

**Component:**
```tsx
<span>{t('sidebar.costFormat', { amount: cost.toFixed(2) })}</span>
```

### Edge Case: Missing Translation Key

If a key `chat:newFeature.title` exists in `en` but not in `es`:
- i18next falls back to English value
- No raw key like `chat:newFeature.title` is ever shown to users
- The translation completeness test catches this gap in CI

### Edge Case: Component Renders Before Translations Load

Since translations are loaded synchronously via static imports (bundled by Vite), this cannot happen in production. The `i18n.init()` call in `config.ts` loads `common` and the default namespace synchronously before `ReactDOM.createRoot()` renders.

### Edge Case: Language Code with Region

Browser may report `es-CO` (Spanish - Colombia). The `i18next-browser-languagedetector` is configured with `lookupFromSubdivisionLocale: true` and falls back to the base language `es`.

### Edge Case: Concurrent Namespace Loading

If a user rapidly switches between Chat and Flows views, multiple namespace imports may fire concurrently. This is safe — dynamic `import()` is idempotent and Vite deduplicates concurrent requests for the same chunk.

## 10. Validation Criteria

1. **Zero Hardcoded Strings**: A grep for common English UI words (e.g., `"Cancel"`, `"Loading"`, `"Sign out"`) in `.tsx` component files returns zero results (excluding translation JSON files and test files).
2. **Translation Parity**: The automated `completeness.test.ts` verifies all keys in `en/*.json` exist in `es/*.json` with non-empty values.
3. **Type Safety**: `tsc --noEmit` passes with no errors related to translation keys. Deliberately using an invalid key like `t('chat:nonexistent.key')` produces a compile error.
4. **Bundle Size**: `npm run build` followed by gzip size analysis shows i18n runtime adds < 15KB gzipped.
5. **Lazy Loading**: Vite build output shows separate chunks for each namespace's translations.
6. **Test Suite**: `npm test` passes with 85%+ coverage on `src/i18n/` and `src/features/i18n/`.
7. **Accessibility**: The language switcher passes axe-core automated accessibility checks with zero violations.
8. **Persistence**: Changing language, closing the tab, and reopening restores the selected language.
9. **HTML Attributes**: `document.documentElement.lang` and `document.documentElement.dir` match the active language at all times.

## 11. Related Specifications / Further Reading

- [spec-design-ux-refresh-navigation.md](spec-design-ux-refresh-navigation.md) — Desktop layout, NavigationRail, StatusBar where language switcher is placed
- [spec-design-landing-page-home-unlogged.md](spec-design-landing-page-home-unlogged.md) — Landing page structure where landing translations apply
- [i18next documentation](https://www.i18next.com/)
- [react-i18next documentation](https://react.i18next.com/)
- [CLDR Plural Rules](https://cldr.unicode.org/index/cldr-spec/plural-rules) — Pluralization rules by language
- [MDN Intl API](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Intl) — Browser internationalization APIs
