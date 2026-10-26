import { useEffect, useState } from 'react';
import { Bell } from 'lucide-react';
import {
  loadPrefs,
  notificationKinds,
  notificationsSupported,
  savePrefs,
  showDesktopNotification,
  type DesktopPrefs,
  type NotificationKind,
} from '../lib/desktop-notifications';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { desktop } from '../lib/platform';

// Alert wording for the desktop app, which shows notifications itself.
const alertText = () => ({
  friendRequest: tr('{name} sent you a friend request'),
  invite: tr('{name} invited you'),
  requestInvite: tr('{name} asked for an invite'),
  response: tr('{name} replied to your invite'),
  other: tr('New VRChat notification'),
  someone: tr('Someone'),
  testTitle: tr('Notifications are on'),
  testBody: tr('New VRChat notifications will show up like this.'),
});

/** Settings for showing VRChat notifications on this device's desktop. */
export default function DesktopNotificationSettings({ notify }: { notify: (message: string) => void }) {
  const locale = useLocale();
  const supported = desktop || notificationsSupported();
  const [prefs, setPrefs] = useState<DesktopPrefs>(loadPrefs);
  const [permission, setPermission] = useState(() =>
    desktop ? 'granted' : supported ? Notification.permission : 'denied',
  );
  useEffect(() => {
    if (!desktop) return;
    // Load the app's settings and keep its alert wording in the current language.
    api('/desktop/notifications', { text: alertText() })
      .then(setPrefs)
      .catch(() => {});
  }, [locale]);
  const update = (next: DesktopPrefs) => {
    setPrefs(next);
    if (desktop)
      void api('/desktop/notifications', { ...next, text: alertText() }).catch((e) => notify(e.message));
    else savePrefs(next);
  };
  async function toggle(on: boolean) {
    if (!on || desktop) return update({ ...prefs, enabled: on });
    const result =
      Notification.permission === 'default'
        ? await Notification.requestPermission()
        : Notification.permission;
    setPermission(result);
    if (result !== 'granted') {
      notify(tr('Notifications are blocked for VRCove. Allow them in your browser’s site settings.'));
      return;
    }
    update({ ...prefs, enabled: true });
  }
  const setKind = (kind: NotificationKind, on: boolean) =>
    update({ ...prefs, kinds: on ? [...prefs.kinds, kind] : prefs.kinds.filter((k) => k !== kind) });
  const active = prefs.enabled && permission === 'granted';
  return (
    <section className="dashboard-panel desktop-notifications">
      <div className="panel-title">
        <h2>{tr('Desktop notifications')}</h2>
        <Bell size={16} />
      </div>
      <div className="settings-content">
        {!supported ? (
          <p>{tr('This browser can’t show desktop notifications.')}</p>
        ) : (
          <>
            <p>
              {tr(
                desktop
                  ? 'Get friend requests, invites and other VRChat notifications on this computer, even with the window closed while VRCove runs in the tray. The app connects to VRChat’s live feed directly.'
                  : 'Get friend requests, invites and other VRChat notifications on this device while VRCove is open in a tab. While a tab is open, VRCove relays VRChat’s live feed using your current session, held in memory only and never saved on the server.',
              )}
            </p>
            {permission === 'denied' && (
              <p className="fineprint">
                {tr('Notifications are blocked for VRCove. Allow them in your browser’s site settings.')}
              </p>
            )}
            <label className="cove-switch">
              <input type="checkbox" checked={active} onChange={(e) => void toggle(e.target.checked)} />
              <span>{tr('Show VRChat notifications on this device')}</span>
            </label>
            <fieldset className="notification-kinds" disabled={!active}>
              <legend>{tr('Notify me about')}</legend>
              {notificationKinds.map(([kind, label]) => (
                <label className="cove-switch" key={kind}>
                  <input
                    type="checkbox"
                    checked={prefs.kinds.includes(kind)}
                    onChange={(e) => setKind(kind, e.target.checked)}
                  />
                  <span>{tr(label)}</span>
                </label>
              ))}
            </fieldset>
            <div className="settings-actions">
              <button
                className="standard-button"
                disabled={!active}
                onClick={() =>
                  desktop
                    ? void api('/desktop/notifications', { action: 'test', text: alertText() }).catch((e) =>
                        notify(e.message),
                      )
                    : void showDesktopNotification(
                        tr('Notifications are on'),
                        tr('New VRChat notifications will show up like this.'),
                        'vrcove-test',
                      )
                }
              >
                <Bell size={14} />
                {tr('Send a test notification')}
              </button>
            </div>
          </>
        )}
      </div>
    </section>
  );
}
