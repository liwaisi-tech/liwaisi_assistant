import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from './loadNamespace';

export function useNamespace(ns: string | string[]) {
  const nsList = Array.isArray(ns) ? ns : [ns];
  const result = useTranslation(nsList as Parameters<typeof useTranslation>[0]);
  const { i18n } = result;
  const namespaces = nsList;

  useEffect(() => {
    Promise.all(namespaces.map(n => loadNamespace(n, i18n.language)));
  }, [i18n.language]); // eslint-disable-line react-hooks/exhaustive-deps

  return result;
}
