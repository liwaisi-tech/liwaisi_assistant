import { useCallback, useRef, useState } from 'react';
import type { A2UIPayload } from '../features/chat/a2ui/types.ts';
import { useFeatureFlag } from './useFeatureFlag.ts';

/** Prefix that marks a message content string as an A2UI payload. */
export const A2UI_MARKER = '$$a2ui:';

/** Feature flag key in localStorage (prefixed by useFeatureFlag). */
export const A2UI_FLAG_KEY = 'a2ui_enabled';

/**
 * Detect whether a content string contains an A2UI payload.
 * When the feature flag is off this always returns false.
 */
export function isA2UIContent(content: string): boolean {
  return content.startsWith(A2UI_MARKER);
}

/**
 * Parse a valid A2UI JSON payload from content.
 * Returns null if the content is not A2UI or contains invalid JSON.
 */
export function parseA2UIPayload(content: string): A2UIPayload | null {
  if (!isA2UIContent(content)) return null;
  try {
    const json = content.slice(A2UI_MARKER.length);
    const parsed: unknown = JSON.parse(json);
    if (
      typeof parsed === 'object' &&
      parsed !== null &&
      'components' in parsed &&
      Array.isArray((parsed as A2UIPayload).components)
    ) {
      return parsed as A2UIPayload;
    }
    return null;
  } catch {
    return null;
  }
}

/**
 * Buffer hook for accumulating partial A2UI JSON during streaming.
 * Chunks are appended; once the buffered content forms a valid
 * A2UI payload it is emitted and the buffer resets.
 */
export function useA2UIBuffer() {
  const bufferRef = useRef('');
  const [payload, setPayload] = useState<A2UIPayload | null>(null);

  const append = useCallback((chunk: string) => {
    bufferRef.current += chunk;
    const parsed = parseA2UIPayload(bufferRef.current);
    if (parsed) {
      setPayload(parsed);
      bufferRef.current = '';
    }
  }, []);

  const reset = useCallback(() => {
    bufferRef.current = '';
    setPayload(null);
  }, []);

  return { payload, append, reset, buffer: bufferRef };
}

/**
 * Main adapter hook that gates all A2UI functionality behind the feature flag.
 * When the flag is off, all detection/parsing functions return false/null,
 * ensuring zero overhead for the standard markdown rendering path.
 */
export function useA2UIAdapter() {
  const enabled = useFeatureFlag(A2UI_FLAG_KEY, false);

  const detect = useCallback(
    (content: string): boolean => {
      if (!enabled) return false;
      return isA2UIContent(content);
    },
    [enabled],
  );

  const parse = useCallback(
    (content: string): A2UIPayload | null => {
      if (!enabled) return null;
      return parseA2UIPayload(content);
    },
    [enabled],
  );

  return { enabled, detect, parse };
}
