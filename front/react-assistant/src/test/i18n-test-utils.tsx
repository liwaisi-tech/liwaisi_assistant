import { type ReactNode } from 'react';
import { I18nextProvider } from 'react-i18next';
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

import commonEn from '../i18n/locales/en/common.json';
import authEn from '../i18n/locales/en/auth.json';
import chatEn from '../i18n/locales/en/chat.json';
import desktopEn from '../i18n/locales/en/desktop.json';
import landingEn from '../i18n/locales/en/landing.json';
import monitorEn from '../i18n/locales/en/monitor.json';
import flowsEn from '../i18n/locales/en/flows.json';
import personalityEn from '../i18n/locales/en/personality.json';
import toolsEn from '../i18n/locales/en/tools.json';
import adminEn from '../i18n/locales/en/admin.json';

import commonEs from '../i18n/locales/es/common.json';
import authEs from '../i18n/locales/es/auth.json';
import chatEs from '../i18n/locales/es/chat.json';
import desktopEs from '../i18n/locales/es/desktop.json';
import landingEs from '../i18n/locales/es/landing.json';
import monitorEs from '../i18n/locales/es/monitor.json';
import flowsEs from '../i18n/locales/es/flows.json';
import personalityEs from '../i18n/locales/es/personality.json';
import toolsEs from '../i18n/locales/es/tools.json';
import adminEs from '../i18n/locales/es/admin.json';

const testI18n = i18n.createInstance();

testI18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: {
      common: commonEn,
      auth: authEn,
      chat: chatEn,
      desktop: desktopEn,
      landing: landingEn,
      monitor: monitorEn,
      flows: flowsEn,
      personality: personalityEn,
      tools: toolsEn,
      admin: adminEn,
    },
    es: {
      common: commonEs,
      auth: authEs,
      chat: chatEs,
      desktop: desktopEs,
      landing: landingEs,
      monitor: monitorEs,
      flows: flowsEs,
      personality: personalityEs,
      tools: toolsEs,
      admin: adminEs,
    },
  },
  defaultNS: 'common',
  interpolation: { escapeValue: false },
});

export { testI18n };

export function I18nTestWrapper({ children }: { children: ReactNode }) {
  return <I18nextProvider i18n={testI18n}>{children}</I18nextProvider>;
}

export function changeTestLanguage(lang: string) {
  return testI18n.changeLanguage(lang);
}
