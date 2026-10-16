import { useSyncExternalStore } from 'react';
import catalogs from './locales/messages.json';

export const languages = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Español' },
  { code: 'fr', name: 'Français' },
  { code: 'de', name: 'Deutsch' },
  { code: 'ja', name: '日本語' },
  { code: 'ru', name: 'Русский' },
  { code: 'uk', name: 'Українська' },
  { code: 'zh-CN', name: '简体中文' },
  { code: 'ko', name: '한국어' },
] as const;
export type Locale = (typeof languages)[number]['code'];
const storageKey = 'vrcove-language',
  legacyStorageKey = 'veilchat-language';
const listeners = new Set<() => void>();
export function resolveLocale(value: string): Locale | undefined {
  const base = value.toLowerCase().split(/[-_]/)[0];
  return languages.find((language) => language.code.toLowerCase().split('-')[0] === base)?.code;
}
function preferredLocale(): Locale {
  try {
    const saved = localStorage.getItem(storageKey) ?? localStorage.getItem(legacyStorageKey);
    if (saved && resolveLocale(saved)) return resolveLocale(saved)!;
  } catch {
    /* Storage is optional. */
  }
  for (const language of navigator.languages || [navigator.language]) {
    const supported = resolveLocale(language);
    if (supported) return supported;
  }
  return 'en';
}
let locale: Locale = preferredLocale();
// Pages opened by path keep the server-rendered title written for search engines.
function updateDocument() {
  document.documentElement.lang = locale;
  if (window.location.pathname === '/')
    document.title = t('VRCove | Private VRChat Web Client with Avatar Search');
}
function updateLocale(next: Locale) {
  if (next === locale) return;
  locale = next;
  updateDocument();
  listeners.forEach((listener) => listener());
}
export function setLocale(next: Locale) {
  if (!languages.some((language) => language.code === next)) return;
  try {
    localStorage.setItem(storageKey, next);
    localStorage.removeItem(legacyStorageKey);
  } catch {
    /* Keep the selection for this session. */
  }
  updateLocale(next);
}
window.addEventListener('storage', (event) => {
  if (event.key === storageKey || event.key === legacyStorageKey) updateLocale(preferredLocale());
});
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};
export function useLocale() {
  return useSyncExternalStore(subscribe, () => locale);
}
export function t(message: string, values: Record<string, string | number> = {}): string {
  const translated =
    locale === 'en'
      ? message
      : (catalogs as Record<string, Record<string, string>>)[locale]?.[message] || message;
  return translated.replace(/\{(\w+)\}/g, (token, key) =>
    values[key] === undefined
      ? token
      : typeof values[key] === 'number'
        ? formatNumber(values[key])
        : String(values[key]),
  );
}
export function formatNumber(value: number) {
  return new Intl.NumberFormat(locale).format(value);
}
export function formatDate(
  value: string,
  options: Intl.DateTimeFormatOptions = { year: 'numeric', month: 'short', day: 'numeric' },
) {
  const date = new Date(value);
  return value && Number.isFinite(date.getTime())
    ? new Intl.DateTimeFormat(locale, options).format(date)
    : t('Not provided');
}
updateDocument();
