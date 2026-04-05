import i18n from './config';

type Loader = () => Promise<{ default: Record<string, unknown> }>;

const loaders: Record<string, Record<string, Loader>> = {
  en: {
    landing: () => import('./locales/en/landing.json'),
    auth: () => import('./locales/en/auth.json'),
    chat: () => import('./locales/en/chat.json'),
    desktop: () => import('./locales/en/desktop.json'),
    monitor: () => import('./locales/en/monitor.json'),
    flows: () => import('./locales/en/flows.json'),
    personality: () => import('./locales/en/personality.json'),
    tools: () => import('./locales/en/tools.json'),
    setup: () => import('./locales/en/setup.json'),
  },
  es: {
    landing: () => import('./locales/es/landing.json'),
    auth: () => import('./locales/es/auth.json'),
    chat: () => import('./locales/es/chat.json'),
    desktop: () => import('./locales/es/desktop.json'),
    monitor: () => import('./locales/es/monitor.json'),
    flows: () => import('./locales/es/flows.json'),
    personality: () => import('./locales/es/personality.json'),
    tools: () => import('./locales/es/tools.json'),
    setup: () => import('./locales/es/setup.json'),
  },
};

const loaded = new Set<string>();

export async function loadNamespace(ns: string, lang?: string): Promise<void> {
  const lng = lang ?? i18n.language;
  const key = `${lng}:${ns}`;
  if (loaded.has(key) || i18n.hasResourceBundle(lng, ns)) return;

  const loader = loaders[lng]?.[ns];
  if (!loader) return;

  const mod = await loader();
  i18n.addResourceBundle(lng, ns, mod.default, true, true);
  loaded.add(key);
}

export function clearLoadedCache() {
  loaded.clear();
}
