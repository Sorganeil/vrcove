import { useState, type ReactNode } from 'react';
import { ArrowUpRight, LogOut, Menu, UserRound } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { sectionLabels } from '../lib/navigation';
import LanguagePicker from './language-picker';
import { SearchIcon } from './nav-icons';
import { CovePicture } from './cove-picture';

/** Header with the current section, language, search and VRCove account. */
export default function Topbar({
  onMenu,
  onSearch,
  accounts,
}: {
  onMenu: () => void;
  onSearch: () => void;
  /** The VRChat account picker, shown first among the actions. */
  accounts?: ReactNode;
}) {
  useLocale();
  const { section, sectionLink, coveAccount, accountChanged, navigate, notify } = useApp();
  const [busy, setBusy] = useState(false);
  async function signOut() {
    setBusy(true);
    try {
      await api('/cove/logout', {});
      accountChanged(null);
      navigate('Home');
      notify(tr('Signed out of VRCove'));
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <header className="topbar">
      <div className="breadcrumb">
        <button className="icon-button mobile-menu" aria-label={tr('Open navigation')} onClick={onMenu}>
          <Menu size={20} />
        </button>
        <a className="topbar-home" aria-label={tr('Home')} {...sectionLink('Home')}>
          <img src="/vrcove-wordmark.svg" alt="" width="88" height="31" />
        </a>
        <span>VRChat</span>
        <span>/</span>
        <strong>{tr(sectionLabels[section] || section)}</strong>
      </div>
      <div className="top-actions">
        {accounts}
        <LanguagePicker compact />
        <button className="header-search" aria-label={tr('Search')} aria-haspopup="dialog" onClick={onSearch}>
          <SearchIcon size={15} />
          <span>{tr('Search VRChat')}</span>
          <kbd>Ctrl K</kbd>
        </button>
        {coveAccount ? (
          <>
            <button className="account-chip" onClick={() => navigate('Account')}>
              <CovePicture picture={coveAccount.picture} username={coveAccount.username} size={29} />
              <span className="account-name">{coveAccount.username}</span>
            </button>
            <button
              className="icon-button sign-out-button"
              aria-label={tr('Sign out of VRCove')}
              onClick={signOut}
              disabled={busy}
            >
              <LogOut size={17} />
            </button>
          </>
        ) : (
          <button
            className="connect-button"
            aria-label={tr('Sign in to VRCove')}
            onClick={() => navigate('Account')}
          >
            <UserRound size={15} />
            <span className="label-long">{tr('Sign in to VRCove')}</span>
            <span className="label-short">{tr('Sign in')}</span>
            <ArrowUpRight size={14} />
          </button>
        )}
      </div>
    </header>
  );
}
