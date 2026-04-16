import { describe, it, expect } from 'vitest';

import { displayPreview } from './previewDisplay';

// Stub TFunction: returns the i18n key verbatim. Adequate for assertions
// that displayPreview routes pathological inputs to the pendingForm key.
const t = ((key: string) => key) as unknown as Parameters<typeof displayPreview>[1];

describe('displayPreview', () => {
  it('passes plain text through unchanged', () => {
    expect(displayPreview('Hola, ¿cómo estás?', t)).toBe('Hola, ¿cómo estás?');
  });

  it('replaces $$a2ui: prefixed strings with the pendingForm key', () => {
    expect(displayPreview('$$a2ui:{"components":[]}', t)).toBe('chat:sidebar.pendingForm');
  });

  it('replaces $$a2ui: prefixed strings even with leading whitespace', () => {
    expect(displayPreview('  \n$$a2ui:{"components":[]}', t)).toBe('chat:sidebar.pendingForm');
  });

  it('replaces bare JSON-looking envelopes with the pendingForm key', () => {
    expect(displayPreview('{"restated_goal":"…"}', t)).toBe('chat:sidebar.pendingForm');
  });

  it('does not match a string that merely contains $$a2ui: mid-text', () => {
    expect(displayPreview('See `$$a2ui:` in the docs.', t)).toBe('See `$$a2ui:` in the docs.');
  });

  it('does not match user prose that begins with a JSON-style quote', () => {
    // String starts with `"` but not `{"` — should not be sanitized.
    expect(displayPreview('"Hola"', t)).toBe('"Hola"');
  });

  it('returns empty string unchanged', () => {
    expect(displayPreview('', t)).toBe('');
  });

  it('never lets the raw $$a2ui: marker reach the output (INV-302)', () => {
    const inputs = [
      '$$a2ui:{"components":[]}',
      '$$a2ui:',
      '$$a2ui:malformed',
      '\n  $$a2ui:{"x":1}',
      '{"components":[]}',
      '{"restated_goal":"…","questions":[]}',
    ];
    for (const input of inputs) {
      const out = displayPreview(input, t);
      expect(out.includes('$$a2ui:')).toBe(false);
    }
  });
});
