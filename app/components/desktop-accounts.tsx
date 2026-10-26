import { useState } from 'react';
import { LoaderCircle, Plus, Users } from 'lucide-react';
import { api, ApiError, resetVRChatSession, useSessionTime } from '../lib/api';
import { useApp } from '../lib/app-context';
import { t as tr, useLocale } from '../lib/i18n';
import { desktop } from '../lib/platform';
import type { LinkedAccount } from '../lib/types';

export function DesktopAccounts({
  onSwitch,
  refresh,
  disabled,
}: {
  onSwitch: (id: number) => Promise<void>;
  refresh: () => Promise<unknown>;
  disabled: boolean;
}) {
  useLocale();
  const { user, linkAccount, requestConnection, navigate, notify } = useApp();
  const links = linkAccount?.links || [];
  const now = useSessionTime();
  const [busy, setBusy] = useState<number | null>(null);
  const reconnect = (link: LinkedAccount) => {
    requestConnection(link.vrchatId, link.username);
  };
  async function select(link: LinkedAccount) {
    if (!link.sessionExpires || link.sessionExpires <= now) {
      reconnect(link);
      return;
    }
    setBusy(link.id);
    try {
      await onSwitch(link.id);
    } catch (e) {
      await refresh();
      if (e instanceof ApiError && e.code === 'vrchat_switch_reconnect') reconnect(link);
      else notify((e as Error).message);
    } finally {
      setBusy(null);
    }
  }
  return (
    <div className="vrchat-account-switcher desktop-account-switcher">
      <label>
        {tr('Active VRChat account')}
        <select
          aria-label={tr('Active VRChat account')}
          value={user?.id || ''}
          disabled={disabled || busy !== null || !links.length}
          onChange={(e) => {
            const link = links.find((l) => l.vrchatId === e.target.value);
            if (link) void select(link);
          }}
        >
          {!user && (
            <option value="">
              {tr(links.length ? 'Choose a connected account' : 'No saved VRChat accounts')}
            </option>
          )}
          {links.map((link) => (
            <option key={link.id} value={link.vrchatId}>
              {link.displayName || link.username || link.vrchatId}
              {!link.sessionExpires || link.sessionExpires <= now
                ? ' (' + tr('Reconnect required') + ')'
                : ''}
            </option>
          ))}
        </select>
      </label>
      <button
        className="standard-button"
        disabled={disabled || busy !== null || links.length >= 10}
        onClick={() => requestConnection('')}
      >
        <Plus size={14} />
        {tr('Add account')}
      </button>
      <button
        className="text-link"
        disabled={disabled || busy !== null}
        onClick={() => navigate('Settings')}
      >
        <Users size={14} />
        {tr('Manage accounts')}
      </button>
    </div>
  );
}

export function DesktopAccountSettings({
  onSwitch,
  refresh,
  disabled = false,
}: {
  onSwitch?: (id: number) => Promise<void>;
  refresh?: () => Promise<unknown>;
  disabled?: boolean;
}) {
  useLocale();
  const { user, setUser, linkAccount, requestConnection, notify } = useApp();
  const links = linkAccount?.links || [];
  const now = useSessionTime();
  const [busy, setBusy] = useState<number | null>(null);
  const [error, setError] = useState('');
  if (!desktop) return null;
  const reconnect = (link: LinkedAccount) => {
    requestConnection(link.vrchatId, link.username);
  };
  async function select(link: LinkedAccount) {
    setError('');
    if (!link.sessionExpires || link.sessionExpires <= now) {
      reconnect(link);
      return;
    }
    if (!onSwitch) return;
    setBusy(link.id);
    try {
      await onSwitch(link.id);
    } catch (e) {
      await refresh?.();
      if (e instanceof ApiError && e.code === 'vrchat_switch_reconnect') reconnect(link);
      else {
        setError((e as Error).message);
        notify((e as Error).message);
      }
    } finally {
      setBusy(null);
    }
  }
  async function remove(link: LinkedAccount) {
    setBusy(link.id);
    setError('');
    try {
      await api('/account/device/remove', { id: link.id });
      if (link.vrchatId === user?.id) {
        resetVRChatSession();
        setUser(null);
      }
      await refresh?.();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  }
  return (
    <section
      className="dashboard-panel desktop-account-manager"
      aria-labelledby="desktop-accounts-title"
    >
      <div className="panel-title">
        <h2 id="desktop-accounts-title">{tr('VRChat accounts on this device')}</h2>
        <Users size={16} />
      </div>
      <div className="settings-content">
        <p>
          {tr(
            'Save up to 10 accounts and switch without entering your password again. Sessions stay encrypted on this device. Passwords are never saved.',
          )}
        </p>
        {links.length ? (
          <ul className="desktop-account-list">
            {links.map((link) => (
              <li key={link.id}>
                <div>
                  <strong>{link.displayName || link.username || link.vrchatId}</strong>
                  <small>{link.username || link.vrchatId}</small>
                  <span className="fineprint">
                    {tr(
                      link.vrchatId === user?.id
                        ? 'Active account'
                        : !link.sessionExpires || link.sessionExpires <= now
                          ? 'Reconnect required'
                          : 'Ready to switch',
                    )}
                  </span>
                </div>
                <div className="desktop-account-actions">
                  {link.vrchatId !== user?.id && (
                    <button
                      className="standard-button"
                      disabled={disabled || busy !== null}
                      onClick={() => void select(link)}
                    >
                      {busy === link.id && <LoaderCircle size={14} className="spin" />}
                      {tr(
                        !link.sessionExpires || link.sessionExpires <= now
                          ? 'Reconnect'
                          : 'Switch account',
                      )}
                    </button>
                  )}
                  <button
                    className="text-link"
                    disabled={disabled || busy !== null}
                    aria-label={tr('Remove {name} from this device', {
                      name: link.displayName || link.username || link.vrchatId,
                    })}
                    onClick={() => void remove(link)}
                  >
                    {tr('Remove')}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p>{tr('No saved VRChat accounts')}</p>
        )}
        {error && (
          <p className="form-error" role="alert">
            {tr(error)}
          </p>
        )}
        <div className="settings-actions">
          <button
            className="standard-button"
            disabled={disabled || busy !== null || links.length >= 10}
            onClick={() => requestConnection('')}
          >
            <Plus size={14} />
            {tr('Add account')}
          </button>
        </div>
        <p className="fineprint">
          {tr(
            'Removing an account forgets its session on this device. You can add it again by signing in.',
          )}
        </p>
      </div>
    </section>
  );
}
