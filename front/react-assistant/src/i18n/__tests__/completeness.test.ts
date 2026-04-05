import { describe, it, expect } from 'vitest';
import { NAMESPACES } from '../config';

// Import all EN and ES locale files
const enModules = import.meta.glob('../locales/en/*.json', { eager: true }) as Record<string, { default: Record<string, unknown> }>;
const esModules = import.meta.glob('../locales/es/*.json', { eager: true }) as Record<string, { default: Record<string, unknown> }>;

function flattenKeys(obj: Record<string, unknown>, prefix = ''): string[] {
  const keys: string[] = [];
  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
      keys.push(...flattenKeys(value as Record<string, unknown>, fullKey));
    } else {
      keys.push(fullKey);
    }
  }
  return keys;
}

function getNamespaceData(modules: Record<string, { default: Record<string, unknown> }>, ns: string): Record<string, unknown> | null {
  for (const [path, mod] of Object.entries(modules)) {
    if (path.endsWith(`/${ns}.json`)) {
      return mod.default ?? (mod as unknown as Record<string, unknown>);
    }
  }
  return null;
}

describe('Translation completeness', () => {
  for (const ns of NAMESPACES) {
    describe(`namespace: ${ns}`, () => {
      const enData = getNamespaceData(enModules, ns);
      const esData = getNamespaceData(esModules, ns);

      it('should have English translation file', () => {
        expect(enData).not.toBeNull();
      });

      it('should have Spanish translation file', () => {
        expect(esData).not.toBeNull();
      });

      if (enData && esData) {
        const enKeys = flattenKeys(enData);
        const esKeys = flattenKeys(esData);

        it('should have all English keys in Spanish', () => {
          const missing = enKeys.filter(k => !esKeys.includes(k));
          expect(missing).toEqual([]);
        });

        it('should not have empty string values in Spanish', () => {
          const emptyKeys: string[] = [];
          function checkEmpty(obj: Record<string, unknown>, prefix = '') {
            for (const [key, value] of Object.entries(obj)) {
              const fullKey = prefix ? `${prefix}.${key}` : key;
              if (typeof value === 'string' && value.trim() === '') {
                emptyKeys.push(fullKey);
              } else if (typeof value === 'object' && value !== null) {
                checkEmpty(value as Record<string, unknown>, fullKey);
              }
            }
          }
          checkEmpty(esData);
          expect(emptyKeys).toEqual([]);
        });

        it('should preserve interpolation variables from English in Spanish', () => {
          const variableMismatches: string[] = [];
          const enFlat = new Map<string, string>();
          function flattenStrings(obj: Record<string, unknown>, prefix = '', target: Map<string, string>) {
            for (const [key, value] of Object.entries(obj)) {
              const fullKey = prefix ? `${prefix}.${key}` : key;
              if (typeof value === 'string') {
                target.set(fullKey, value);
              } else if (typeof value === 'object' && value !== null) {
                flattenStrings(value as Record<string, unknown>, fullKey, target);
              }
            }
          }
          const esFlat = new Map<string, string>();
          flattenStrings(enData, '', enFlat);
          flattenStrings(esData, '', esFlat);

          for (const [key, enValue] of enFlat) {
            const enVars = (enValue.match(/\{\{(\w+)\}\}/g) ?? []).sort();
            const esValue = esFlat.get(key);
            if (esValue) {
              const esVars = (esValue.match(/\{\{(\w+)\}\}/g) ?? []).sort();
              if (JSON.stringify(enVars) !== JSON.stringify(esVars)) {
                variableMismatches.push(`${key}: EN has ${enVars.join(',')} but ES has ${esVars.join(',')}`);
              }
            }
          }
          expect(variableMismatches).toEqual([]);
        });
      }
    });
  }
});
