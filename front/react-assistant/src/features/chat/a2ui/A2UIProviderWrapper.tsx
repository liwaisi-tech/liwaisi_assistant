import { lazy, Suspense, createContext, useContext } from 'react';
import type { A2UIPayload, A2UIAction } from './types.ts';

// ── Component catalog context ──────────────────────────────────────────────

/**
 * Context that carries the BRAE component catalog.
 * Currently unused downstream but wired for future A2UI SDK integration,
 * where external consumers may need to access the catalog for custom rendering.
 */
interface A2UICatalogContextValue {
  catalogVersion: string;
}

const A2UICatalogContext = createContext<A2UICatalogContextValue>({
  catalogVersion: '1.0',
});

export function useA2UICatalog(): A2UICatalogContextValue {
  return useContext(A2UICatalogContext);
}

// ── Lazy-loaded renderer ───────────────────────────────────────────────────

const LazyA2UIMessageRenderer = lazy(() =>
  import('./A2UIMessageRenderer.tsx').then((mod) => ({
    default: mod.A2UIMessageRenderer,
  })),
);

// ── Loading fallback ───────────────────────────────────────────────────────

function A2UILoadingFallback() {
  return (
    <div className="flex items-center gap-2 py-2">
      <div className="thinking-dot" />
      <div className="thinking-dot" />
      <div className="thinking-dot" />
    </div>
  );
}

// ── Provider wrapper ───────────────────────────────────────────────────────

interface A2UIProviderWrapperProps {
  payload: A2UIPayload;
  isStreaming: boolean;
  onAction: (action: A2UIAction) => void;
}

/**
 * Lazy-loaded wrapper for A2UI rendering.
 * When rendered, it code-splits the A2UIMessageRenderer so the main bundle
 * is not penalized when A2UI is unused. Provides the BRAE catalog context.
 */
export function A2UIProviderWrapper({ payload, isStreaming, onAction }: A2UIProviderWrapperProps) {
  return (
    <A2UICatalogContext.Provider value={{ catalogVersion: '1.0' }}>
      <Suspense fallback={<A2UILoadingFallback />}>
        <LazyA2UIMessageRenderer payload={payload} isStreaming={isStreaming} onAction={onAction} />
      </Suspense>
    </A2UICatalogContext.Provider>
  );
}
