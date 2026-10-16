import { formatDate, formatNumber } from './i18n';

export const date = (s?: string) =>
  s && Number.isFinite(new Date(s).getTime())
    ? formatDate(/^\d{4}-\d{2}-\d{2}$/.test(s) ? s + 'T12:00:00' : s)
    : '-';

export const count = (n?: number) => (typeof n === 'number' ? formatNumber(n) : '?');

export const regionNames: Record<string, string> = {
  us: 'US West',
  use: 'US East',
  eu: 'Europe',
  jp: 'Japan',
};

export const regionOf = (instance: string) => /~region\(([a-z]+)\)/.exec(instance)?.[1] || 'us';

export const platformName = (p: string) =>
  p === 'standalonewindows' ? 'PC' : p === 'android' ? 'Android / Quest' : p === 'ios' ? 'iOS' : p;

export const safeLinks = (links: unknown) =>
  (Array.isArray(links) ? links : []).filter(
    (l: unknown): l is string => typeof l === 'string' && /^https:\/\//.test(l),
  );

export const joinStates: Record<string, string> = {
  open: 'Anyone can join',
  request: 'Join by request',
  invite: 'Invite only',
  closed: 'Closed',
};

export function trustRank(tags: string[] = []) {
  if (tags.includes('system_trust_veteran')) return 'Trusted User';
  if (tags.includes('system_trust_trusted')) return 'Known User';
  if (tags.includes('system_trust_known')) return 'User';
  if (tags.includes('system_trust_basic')) return 'New User';
  return 'Visitor';
}

export const userImage = (u: any) =>
  u.profilePicOverride || u.userIcon || u.currentAvatarImageUrl || u.currentAvatarThumbnailImageUrl;

export const userThumb = (u: any) => u.userIcon || u.profilePicOverride || u.currentAvatarThumbnailImageUrl;
