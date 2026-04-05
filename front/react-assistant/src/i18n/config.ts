import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import commonEn from './locales/en/common.json';
import commonEs from './locales/es/common.json';

export interface LanguageMeta {
  code: string;
  name: string;
  dir: 'ltr' | 'rtl';
}

export const SUPPORTED_LANGUAGES: LanguageMeta[] = [
  { code: 'es', name: 'Español', dir: 'ltr' },
  { code: 'en', name: 'English', dir: 'ltr' },
];

export const DEFAULT_LANGUAGE = 'es';
export const LANGUAGE_STORAGE_KEY = 'liwaisi_lang';

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

export type Namespace = (typeof NAMESPACES)[number];

function syncDocumentLang(lang: string) {
  const meta = SUPPORTED_LANGUAGES.find(l => l.code === lang);
  document.documentElement.lang = lang;
  document.documentElement.dir = meta?.dir ?? 'ltr';
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { common: commonEn },
      es: { common: commonEs },
    },
    defaultNS: 'common',
    fallbackLng: 'en',
    supportedLngs: SUPPORTED_LANGUAGES.map(l => l.code),
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: LANGUAGE_STORAGE_KEY,
      caches: ['localStorage'],
    },
  });

syncDocumentLang(i18n.language);
i18n.on('languageChanged', syncDocumentLang);

export default i18n;
