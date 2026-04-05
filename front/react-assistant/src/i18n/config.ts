import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

// Bundle all namespaces synchronously — ensures translations are available on first render
import commonEn from './locales/en/common.json';
import landingEn from './locales/en/landing.json';
import authEn from './locales/en/auth.json';
import chatEn from './locales/en/chat.json';
import desktopEn from './locales/en/desktop.json';
import monitorEn from './locales/en/monitor.json';
import flowsEn from './locales/en/flows.json';
import personalityEn from './locales/en/personality.json';
import toolsEn from './locales/en/tools.json';
import adminEn from './locales/en/admin.json';

import commonEs from './locales/es/common.json';
import landingEs from './locales/es/landing.json';
import authEs from './locales/es/auth.json';
import chatEs from './locales/es/chat.json';
import desktopEs from './locales/es/desktop.json';
import monitorEs from './locales/es/monitor.json';
import flowsEs from './locales/es/flows.json';
import personalityEs from './locales/es/personality.json';
import toolsEs from './locales/es/tools.json';
import adminEs from './locales/es/admin.json';

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
  'admin',
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
      en: {
        common: commonEn,
        landing: landingEn,
        auth: authEn,
        chat: chatEn,
        desktop: desktopEn,
        monitor: monitorEn,
        flows: flowsEn,
        personality: personalityEn,
        tools: toolsEn,
        admin: adminEn,
      },
      es: {
        common: commonEs,
        landing: landingEs,
        auth: authEs,
        chat: chatEs,
        desktop: desktopEs,
        monitor: monitorEs,
        flows: flowsEs,
        personality: personalityEs,
        tools: toolsEs,
        admin: adminEs,
      },
    },
    defaultNS: 'common',
    fallbackLng: 'en',
    lng: DEFAULT_LANGUAGE,
    supportedLngs: SUPPORTED_LANGUAGES.map(l => l.code),
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage'],
      lookupLocalStorage: LANGUAGE_STORAGE_KEY,
      caches: ['localStorage'],
    },
  });

syncDocumentLang(i18n.language);
i18n.on('languageChanged', syncDocumentLang);

export default i18n;
