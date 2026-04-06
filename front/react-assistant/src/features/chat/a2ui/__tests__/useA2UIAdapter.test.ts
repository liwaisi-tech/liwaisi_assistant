import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  A2UI_MARKER,
  A2UI_FLAG_KEY,
  isA2UIContent,
  parseA2UIPayload,
  useA2UIBuffer,
  useA2UIAdapter,
} from '../../../../hooks/useA2UIAdapter';

// ── Pure function tests ────────────────────────────────────────────────────

describe('isA2UIContent', () => {
  it('returns true for content starting with A2UI marker', () => {
    expect(isA2UIContent(`${A2UI_MARKER}{"components":[]}`)).toBe(true);
  });

  it('returns false for plain text', () => {
    expect(isA2UIContent('Hello world')).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isA2UIContent('')).toBe(false);
  });

  it('returns false for partial marker', () => {
    expect(isA2UIContent('$$a2u')).toBe(false);
  });
});

describe('parseA2UIPayload', () => {
  it('parses valid A2UI payload', () => {
    const payload = { components: [{ type: 'text', props: { content: 'hello' } }] };
    const result = parseA2UIPayload(`${A2UI_MARKER}${JSON.stringify(payload)}`);
    expect(result).toEqual(payload);
  });

  it('returns null for non-A2UI content', () => {
    expect(parseA2UIPayload('just some text')).toBeNull();
  });

  it('returns null for malformed JSON after marker', () => {
    expect(parseA2UIPayload(`${A2UI_MARKER}{invalid json`)).toBeNull();
  });

  it('returns null when JSON is valid but missing components array', () => {
    expect(parseA2UIPayload(`${A2UI_MARKER}{"data":"no components"}`)).toBeNull();
  });

  it('returns null when components is not an array', () => {
    expect(parseA2UIPayload(`${A2UI_MARKER}{"components":"not-array"}`)).toBeNull();
  });

  it('preserves optional data field', () => {
    const payload = { components: [], data: { key: 'value' } };
    const result = parseA2UIPayload(`${A2UI_MARKER}${JSON.stringify(payload)}`);
    expect(result?.data).toEqual({ key: 'value' });
  });
});

// ── useA2UIBuffer hook tests ───────────────────────────────────────────────

describe('useA2UIBuffer', () => {
  it('emits null payload initially', () => {
    const { result } = renderHook(() => useA2UIBuffer());
    expect(result.current.payload).toBeNull();
  });

  it('accumulates partial chunks and emits complete payload', () => {
    const { result } = renderHook(() => useA2UIBuffer());
    const fullPayload = `${A2UI_MARKER}${JSON.stringify({ components: [{ type: 'text', props: { content: 'hi' } }] })}`;

    // Send first half
    act(() => {
      result.current.append(fullPayload.slice(0, 20));
    });
    expect(result.current.payload).toBeNull();

    // Send second half
    act(() => {
      result.current.append(fullPayload.slice(20));
    });
    expect(result.current.payload).not.toBeNull();
    expect(result.current.payload?.components[0].type).toBe('text');
  });

  it('resets buffer and payload', () => {
    const { result } = renderHook(() => useA2UIBuffer());
    const fullPayload = `${A2UI_MARKER}${JSON.stringify({ components: [] })}`;

    act(() => {
      result.current.append(fullPayload);
    });
    expect(result.current.payload).not.toBeNull();

    act(() => {
      result.current.reset();
    });
    expect(result.current.payload).toBeNull();
  });
});

// ── useA2UIAdapter hook tests ──────────────────────────────────────────────

describe('useA2UIAdapter', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('returns disabled when feature flag is off', () => {
    const { result } = renderHook(() => useA2UIAdapter());
    expect(result.current.enabled).toBe(false);
  });

  it('detect returns false when feature flag is off', () => {
    const { result } = renderHook(() => useA2UIAdapter());
    expect(result.current.detect(`${A2UI_MARKER}{"components":[]}`)).toBe(false);
  });

  it('parse returns null when feature flag is off', () => {
    const { result } = renderHook(() => useA2UIAdapter());
    expect(result.current.parse(`${A2UI_MARKER}{"components":[]}`)).toBeNull();
  });

  it('detect returns true when flag is on and content is A2UI', () => {
    localStorage.setItem(`liwaisi_${A2UI_FLAG_KEY}`, 'true');
    const { result } = renderHook(() => useA2UIAdapter());
    expect(result.current.detect(`${A2UI_MARKER}{"components":[]}`)).toBe(true);
  });

  it('parse returns payload when flag is on and content is valid', () => {
    localStorage.setItem(`liwaisi_${A2UI_FLAG_KEY}`, 'true');
    const payload = { components: [{ type: 'card', props: {} }] };
    const { result } = renderHook(() => useA2UIAdapter());
    expect(result.current.parse(`${A2UI_MARKER}${JSON.stringify(payload)}`)).toEqual(payload);
  });
});
