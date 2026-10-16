import { formatDate } from './i18n';

export const day = (s: string) => {
  const d = new Date(s + 'T00:00:00Z');
  return isNaN(+d)
    ? s
    : formatDate(d.toISOString(), { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' });
};
