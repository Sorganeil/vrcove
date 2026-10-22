import { t as tr, useLocale } from '../lib/i18n';

type Presence = { status?: string; statusDescription?: string; state?: string; location?: string };

/**
 * Online on the website, the app or a tool like VRCX, but not in VRChat itself.
 * VRChat marks these people "active" (in game is "online") and gives no location.
 */
export function notInGame(user: Presence, offline?: boolean) {
  if (offline === true || user.state === 'offline' || user.status === 'offline') return false;
  return user.state === 'active' || user.location === 'offline';
}

export function userPresence(user: Presence, offline?: boolean) {
  if (notInGame(user, offline)) return { tone: 'web', label: 'Not in game' };
  const away =
    offline === true || user.state === 'offline' || user.location === 'offline' || user.status === 'offline';
  const statuses: Record<string, { tone: string; label: string }> = {
    active: { tone: 'active', label: 'Online' },
    'join me': { tone: 'join-me', label: 'Join me' },
    'ask me': { tone: 'ask-me', label: 'Ask me' },
    busy: { tone: 'busy', label: 'Do not disturb' },
  };
  return away
    ? { tone: 'offline', label: 'Offline' }
    : statuses[user.status || ''] || { tone: 'unknown', label: 'Status unavailable' };
}

export default function UserStatus({ user, offline }: { user: Presence; offline?: boolean }) {
  useLocale();
  const presence = userPresence(user, offline);
  return (
    <span className={'user-status tone-' + presence.tone}>
      <span className="user-availability">
        <i className={'presence-dot ' + presence.tone} aria-hidden="true" />
        {tr(presence.label)}
      </span>
      {user.statusDescription && <span className="user-status-message">{user.statusDescription}</span>}
    </span>
  );
}
