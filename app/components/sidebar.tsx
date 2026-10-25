import type { ComponentType } from 'react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { navigationGroups, primaryNavigation, accountNavigation } from '../lib/navigation';
import { ShieldCheck } from 'lucide-react';
import InstallApp from './install-app';
import { desktop } from '../lib/platform';

type NavItem = { name: string; label: string; icon: ComponentType<{ size?: number }> };

/** Main navigation. On phones it slides in when `open`. */
export default function Sidebar({ open }: { open: boolean }) {
  useLocale();
  const { section, sectionLink, saved, openPrivacy, coveAccount } = useApp();
  const staff = coveAccount?.role === 'admin' || coveAccount?.role === 'owner';
  function item(entry: NavItem) {
    const active = section === entry.name;
    return (
      <a
        key={entry.name}
        className={
          'nav-item ' + (active ? 'active' : '') + (entry.name === 'Companion' ? ' desktop-only' : '')
        }
        aria-current={active ? 'page' : undefined}
        {...sectionLink(entry.name)}
      >
        <entry.icon size={18} />
        <span>{tr(desktop && entry.name === 'Companion' ? 'Avatar logger' : entry.label)}</span>
        {entry.name === 'Bookmarks' && saved.length > 0 && <small>{saved.length}</small>}
      </a>
    );
  }
  return (
    <aside className={'sidebar ' + (open ? 'open' : '')}>
      <a className="brand" {...sectionLink('Home')}>
        <img className="cove-wordmark" src="/vrcove-wordmark.svg" alt="VRCove" width="180" height="63" />
      </a>
      <nav className="sidebar-navigation" aria-label={tr('Main navigation')}>
        <div className="nav-primary">{primaryNavigation.map(item)}</div>
        {navigationGroups.map((group) => (
          <section className="nav-group" key={group.label} aria-label={tr(group.label)}>
            <h2 className="nav-label">{tr(group.label)}</h2>
            {group.items.map(item)}
          </section>
        ))}
      </nav>
      <div className="sidebar-bottom">
        {!desktop && <InstallApp />}
        <nav aria-label={tr('Account')}>
          {accountNavigation.map(item)}
          {staff && item({ name: 'Admin', label: 'Moderation', icon: ShieldCheck })}
        </nav>
        <div className="sidebar-links">
          <a aria-current={section === 'About' ? 'page' : undefined} {...sectionLink('About')}>
            {tr('About')}
          </a>
          <a aria-current={section === 'Donations' ? 'page' : undefined} {...sectionLink('Donations')}>
            {tr('Donations')}
          </a>
          <button onClick={openPrivacy}>{tr('Privacy & data')}</button>
        </div>
      </div>
    </aside>
  );
}
