import { describe, it, expect, afterEach } from 'vitest';
import i18n from '../config';
import { SUPPORTED_LANGUAGES, DEFAULT_LANGUAGE, LANGUAGE_STORAGE_KEY } from '../config';

describe('i18n config', () => {
  const originalLang = document.documentElement.lang;
  const originalDir = document.documentElement.dir;

  afterEach(() => {
    document.documentElement.lang = originalLang;
    document.documentElement.dir = originalDir;
    localStorage.removeItem(LANGUAGE_STORAGE_KEY);
    i18n.changeLanguage('es');
  });

  it('should initialize with Spanish as default language', () => {
    expect(DEFAULT_LANGUAGE).toBe('es');
  });

  it('should have common namespace loaded', () => {
    expect(i18n.hasResourceBundle('en', 'common')).toBe(true);
    expect(i18n.hasResourceBundle('es', 'common')).toBe(true);
  });

  it('should translate common keys in Spanish', () => {
    expect(i18n.t('buttons.cancel')).toBe('Cancelar');
  });

  it('should translate common keys in English', async () => {
    await i18n.changeLanguage('en');
    expect(i18n.t('buttons.cancel')).toBe('Cancel');
    expect(i18n.t('status.connected')).toBe('Connected');
    expect(i18n.t('status.idle')).toBe('Idle');
  });

  it('should fall back to English for unsupported languages', async () => {
    await i18n.changeLanguage('fr');
    expect(i18n.language).toBe('en');
    expect(i18n.t('buttons.cancel')).toBe('Cancel');
  });

  it('should update document lang attribute on language change', async () => {
    await i18n.changeLanguage('es');
    expect(document.documentElement.lang).toBe('es');
  });

  it('should update document dir attribute on language change', async () => {
    await i18n.changeLanguage('es');
    expect(document.documentElement.dir).toBe('ltr');
  });

  it('should persist language choice to localStorage', async () => {
    await i18n.changeLanguage('es');
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe('es');
  });

  it('should define supported languages with required fields', () => {
    expect(SUPPORTED_LANGUAGES.length).toBeGreaterThanOrEqual(2);
    for (const lang of SUPPORTED_LANGUAGES) {
      expect(lang.code).toBeTruthy();
      expect(lang.name).toBeTruthy();
      expect(['ltr', 'rtl']).toContain(lang.dir);
    }
  });

  it('should list Spanish first in supported languages', () => {
    expect(SUPPORTED_LANGUAGES[0].code).toBe('es');
  });

  it('should support interpolation', async () => {
    await i18n.changeLanguage('en');
    expect(i18n.t('timeAgo.minutesAgo', { count: 5 })).toBe('5m ago');
  });
});
