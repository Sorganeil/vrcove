import { useState } from 'react';
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
import { t as tr } from '../lib/i18n';

/** Settings for showing VRChat notifications on this device's desktop. */
export default function DesktopNotificationSettings({ notify }: { notify: (message: string) => void }) {
  const supported = notificationsSupported();
  const [prefs, setPrefs] = useState<DesktopPrefs>(loadPrefs);
  const [permission, setPermission] = useState(() => (supported ? Notification.permission : 'denied'));
  const update = (next: DesktopPrefs) => {
    setPrefs(next);
    savePrefs(next);
  };
  async function toggle(on: boolean) {
    if (!on) return update({ ...prefs, enabled: false });
    const result = Notification.permission === 'default' ? await Notification.requestPermission() : Notification.permission;
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
                'Get friend requests, invites and other VRChat notifications on this device while VRCove is open in a tab. While a tab is open, VRCove relays VRChat’s live feed using your current session, held in memory only and never saved on the server.',
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
                  void showDesktopNotification(
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
