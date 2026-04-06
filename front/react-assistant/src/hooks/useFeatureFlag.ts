import { useSyncExternalStore } from 'react';

const PREFIX = 'liwaisi_';

function getSnapshot(key: string, defaultValue: boolean): () => boolean {
  return () => {
    try {
      const raw = globalThis.localStorage?.getItem(`${PREFIX}${key}`);
      if (raw === null) return defaultValue;
      return raw === 'true';
    } catch {
      return defaultValue;
    }
  };
}

function getServerSnapshot(defaultValue: boolean): () => boolean {
  return () => defaultValue;
}

function subscribe(callback: () => void): () => void {
  window.addEventListener('storage', callback);
  return () => window.removeEventListener('storage', callback);
}

/**
 * Simple localStorage-based feature flag hook.
 * Reads from localStorage with 'liwaisi_' prefix.
 * SSR-safe (defaults to provided defaultValue on server).
 * Re-renders on cross-tab storage events.
 */
export function useFeatureFlag(key: string, defaultValue = false): boolean {
  return useSyncExternalStore(
    subscribe,
    getSnapshot(key, defaultValue),
    getServerSnapshot(defaultValue),
  );
}
