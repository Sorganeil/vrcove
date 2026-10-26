import { useEffect, useState } from 'react';
import { api } from './api';
import { t as tr } from './i18n';
import { desktop } from './platform';
import { goTo } from './routes';

/** Kinds of VRChat notification people can choose to see on their desktop. */
export const notificationKinds = [
  ['friendRequest', 'Friend requests'],
  ['invite', 'Invites'],
  ['requestInvite', 'Invite requests'],
  ['response', 'Replies to your invites'],
  ['other', 'Other VRChat notifications'],
] as const;
export type NotificationKind = (typeof notificationKinds)[number][0];
export type DesktopPrefs = { enabled: boolean; kinds: NotificationKind[] };

const prefsKey = 'vrcove-desktop-notifications';
const seenKey = (user: string) => 'vrcove-notifications-seen:' + user;
const changed = 'vrcove:desktop-notifications';
const defaults: DesktopPrefs = {
  enabled: false,
  kinds: ['friendRequest', 'invite', 'requestInvite', 'response'],
};
// VRChat asks third-party apps to check no more than once a minute.
const every = 60_000;

export const notificationsSupported = () => typeof window !== 'undefined' && 'Notification' in window;

export function loadPrefs(): DesktopPrefs {
  try {
    const saved = JSON.parse(localStorage.getItem(prefsKey) || 'null');
    if (saved && typeof saved.enabled === 'boolean' && Array.isArray(saved.kinds)) return saved;
  } catch {
    /* Storage unavailable or damaged: use the defaults. */
  }
  return defaults;
}

/** Saves this device's choices and tells the open checker at once. */
export function savePrefs(prefs: DesktopPrefs) {
  try {
    localStorage.setItem(prefsKey, JSON.stringify(prefs));
  } catch {
    /* Settings still apply until the page closes. */
  }
  window.dispatchEvent(new Event(changed));
}

function kindOf(type: string): NotificationKind {
  if (type === 'friendRequest' || type === 'invite' || type === 'requestInvite') return type;
  if (type === 'inviteResponse' || type === 'requestInviteResponse') return 'response';
  return 'other';
}

type VRChatNotification = {
  id: string;
  type: string;
  senderUsername?: string;
  message?: string;
  details?: { worldName?: string } | string;
};

function describe(n: VRChatNotification) {
  const name = n.senderUsername || tr('Someone');
  const details = typeof n.details === 'object' ? n.details : undefined;
  switch (kindOf(n.type)) {
    case 'friendRequest':
      return { title: tr('{name} sent you a friend request', { name }), body: '' };
    case 'invite':
      return { title: tr('{name} invited you', { name }), body: details?.worldName || n.message || '' };
    case 'requestInvite':
      return { title: tr('{name} asked for an invite', { name }), body: n.message || '' };
    case 'response':
      return { title: tr('{name} replied to your invite', { name }), body: n.message || '' };
    default:
      return { title: name, body: n.message || tr('New VRChat notification') };
  }
}

/** Shows one desktop notification; clicking it opens the Notifications page. */
export async function showDesktopNotification(title: string, body: string, tag: string) {
  const options = { body, tag, icon: '/icon-192.png' };
  try {
    const note = new Notification(title, options);
    note.onclick = () => {
      window.focus();
      goTo('/notifications');
      note.close();
    };
  } catch {
    // Phones and installed apps only allow notifications through the service worker.
    const registration = await navigator.serviceWorker?.ready;
    await registration?.showNotification(title, { ...options, data: { url: '/notifications' } });
  }
}

const liveLock = 'vrcove-live-notifications';
const maxSeen = 200;

function readSeen(userId: string): Set<string> | null {
  try {
    const saved = localStorage.getItem(seenKey(userId));
    return saved ? new Set(JSON.parse(saved)) : null;
  } catch {
    return null;
  }
}
function writeSeen(userId: string, ids: string[]) {
  try {
    localStorage.setItem(seenKey(userId), JSON.stringify(ids.slice(-maxSeen)));
  } catch {
    /* Nothing else to do. */
  }
}

/** Shows n unless it was shown before or its kind is switched off, and remembers it. */
async function announce(userId: string, n: VRChatNotification) {
  const seen = readSeen(userId) ?? new Set<string>();
  if (seen.has(n.id)) return;
  writeSeen(userId, [...seen, n.id]);
  const prefs = loadPrefs();
  if (!prefs.enabled || !prefs.kinds.includes(kindOf(n.type)) || Notification.permission !== 'granted')
    return;
  const { title, body } = describe(n);
  await showDesktopNotification(title, body, 'vrchat-' + n.id);
}

/** Fetches the notification list and shows any not seen before. The first check only records a baseline. */
async function catchUp(userId: string) {
  const list: VRChatNotification[] = await api('/account/notifications');
  if (!readSeen(userId)) {
    writeSeen(
      userId,
      list.map((n) => n.id),
    );
    return;
  }
  for (const n of list) await announce(userId, n);
}

/**
 * While VRCove is open, shows new VRChat notifications of the chosen kinds on
 * the desktop as they arrive. One tab holds a live connection through VRCove,
 * which relays VRChat's own live feed using this browser's session, kept only
 * in memory while the connection is open. Without that, or while it is down,
 * the tab checks once a minute instead, as VRChat asks of third-party apps.
 */
export function useDesktopNotifications(userId: string | undefined) {
  const [enabled, setEnabled] = useState(() => loadPrefs().enabled);
  useEffect(() => {
    const refresh = () => setEnabled(loadPrefs().enabled);
    window.addEventListener(changed, refresh);
    window.addEventListener('storage', refresh);
    return () => {
      window.removeEventListener(changed, refresh);
      window.removeEventListener('storage', refresh);
    };
  }, []);
  useEffect(() => {
    // The desktop app shows notifications itself, even with the window closed.
    if (desktop || !userId || !enabled || !notificationsSupported() || Notification.permission !== 'granted')
      return;
    const stop = new AbortController();
    const wait = (ms: number) =>
      new Promise<void>((resolve) => {
        const timer = window.setTimeout(resolve, ms);
        stop.signal.addEventListener('abort', () => {
          window.clearTimeout(timer);
          resolve();
        });
      });
    const poll = async (until: () => boolean) => {
      while (!stop.signal.aborted && !until()) {
        await wait(every);
        if (!stop.signal.aborted) await catchUp(userId).catch(() => undefined);
      }
    };
    // Opens the live feed and resolves once it closes; true if it stayed up for a while.
    const listen = () =>
      new Promise<boolean>((resolve) => {
        const opened = Date.now();
        const source = new EventSource('/api/account/live');
        const close = () => {
          source.close();
          resolve(Date.now() - opened > 2 * every);
        };
        source.onmessage = (event) => {
          try {
            void announce(userId, JSON.parse(event.data));
          } catch {
            /* Ignore a damaged message. */
          }
        };
        source.addEventListener('ended', close);
        source.onerror = close;
        stop.signal.addEventListener('abort', close);
      });
    const run = async () => {
      let delay = 10_000,
        behind = true;
      while (!stop.signal.aborted) {
        // Pick up anything that arrived while no live connection was open.
        if (behind) await catchUp(userId).catch(() => undefined);
        if (typeof EventSource === 'undefined') return poll(() => false);
        if (await listen()) {
          delay = 10_000;
          behind = true;
          continue;
        }
        // Check once a minute until it is time to try the live feed again.
        const retry = Date.now() + delay;
        delay = Math.min(delay * 2, 5 * every);
        behind = false;
        await poll(() => Date.now() >= retry);
      }
    };
    // Several tabs share one connection: whichever holds the lock listens.
    if (navigator.locks)
      navigator.locks.request(liveLock, { signal: stop.signal }, run).catch(() => undefined);
    else void run();
    return () => stop.abort();
  }, [userId, enabled]);
}
