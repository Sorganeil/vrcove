import { desktop } from '../lib/platform';
import { useState } from 'react';
import { ArrowRight, ShieldCheck, X } from 'lucide-react';
import { api, useSessionTime, useVRChatSession, vrchatAccountState } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { DesktopAccounts } from './desktop-accounts';

/**
 * Banners above every page: the link reminder and the connection notice. A dismissed connection notice stays hidden until the connection changes.
 */
export default function SessionNotices() {
  useLocale();
  const { coveAccount, linkAccount, setCoveAccount, requestConnection, notify } = useApp();
  const connection = useVRChatSession();
  const [hiddenNotice, setHiddenNotice] = useState<object | null>(null);
  return (
    <>
      {!desktop && coveAccount?.showLinkReminder && !coveAccount?.links?.length && (
        <div className="vrchat-session-notice" role="status">
          <ShieldCheck size={22} />
          <div>
            <strong>{tr('Get the most out of VRCove')}</strong>
            <p>{tr('Link a VRChat account to use friends, favorites, avatar switching and more.')}</p>
          </div>
          <button
            className="standard-button"
            onClick={() => {
              requestConnection();
            }}
          >
            {tr('Link VRChat now')}
            <ArrowRight size={14} />
          </button>
          <button
            className="text-link"
            onClick={() => {
              api('/cove/link-reminder/dismiss', {})
                .then(setCoveAccount)
                .catch((e) => notify(e.message));
            }}
          >
            {tr('Maybe later')}
          </button>
        </div>
      )}
      {linkAccount && connection.issue && hiddenNotice !== connection && (
        <div className="vrchat-session-notice" role="alert">
          <ShieldCheck size={22} />
          <div>
            <strong>
              {tr(
                connection.issue === 'vrchat_session_expired'
                  ? 'VRChat session expired'
                  : connection.issue === 'vrchat_session_invalid'
                    ? 'VRChat session is no longer valid'
                    : 'Connect VRChat on this device',
              )}
            </strong>
            <p>
              {tr(
                connection.issue === 'vrchat_session_required'
                  ? 'VRChat connects separately on each device, so this one needs to connect once. Your VRChat account is still linked, and you’re still signed in to VRCove.'
                  : 'Reconnect VRChat to use friends, favorites and other VRChat features. You’re still signed in to VRCove, and your accounts remain linked.',
              )}
            </p>
          </div>
          <button className="standard-button" onClick={() => requestConnection()}>
            {tr(connection.issue === 'vrchat_session_required' ? 'Connect VRChat' : 'Reconnect VRChat')}
            <ArrowRight size={14} />
          </button>
          <button
            className="icon-button notice-dismiss"
            aria-label={tr('Dismiss')}
            onClick={() => setHiddenNotice(connection)}
          >
            <X size={16} />
          </button>
        </div>
      )}
    </>
  );
}

/** The active VRChat account picker in the header; Manage accounts is in the same menu. */
export function AccountSwitcher({
  onSwitch,
  switching,
  connecting,
  refresh,
}: {
  onSwitch: (id: number) => Promise<void>;
  switching: boolean;
  connecting: boolean;
  refresh: () => Promise<unknown>;
}) {
  useLocale();
  const { user, coveAccount, setCoveAccount, navigate, notify } = useApp();
  const connection = useVRChatSession();
  const now = useSessionTime();
  if (desktop)
    return <DesktopAccounts onSwitch={onSwitch} refresh={refresh} disabled={switching || connecting} />;
  if (!coveAccount?.links.length) return null;
  return (
    <label className="vrchat-account-switcher">
      <span className="sr-only">{tr('Active VRChat account')}</span>
      <select
        aria-label={tr('Active VRChat account')}
        value={user?.id || ''}
        disabled={switching || connecting}
        onChange={(e) => {
          if (e.target.value === '__manage') {
            navigate('Account');
            return;
          }
          const link = coveAccount.links.find((l) => l.vrchatId === e.target.value);
          if (link)
            void onSwitch(link.id).catch(async (e) => {
              notify(e.message);
              setCoveAccount(await api('/cove/me'));
            });
        }}
      >
        {!user && <option value="">{tr('Choose a connected account')}</option>}
        {coveAccount.links.map((l) => (
          <option value={l.vrchatId} key={l.id}>
            {l.displayName || l.vrchatId}
            {vrchatAccountState(l, user?.id, connection, now) !== 'reconnect'
              ? ''
              : ' · ' + tr('Reconnect required')}
          </option>
        ))}
        <option value="__manage">{tr('Manage accounts')}</option>
      </select>
    </label>
  );
}
