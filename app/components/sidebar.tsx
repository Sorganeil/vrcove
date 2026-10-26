import { useState, type ComponentType } from 'react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { navigationGroups, primaryNavigation, accountNavigation } from '../lib/navigation';
import { ChevronDown, ShieldCheck } from 'lucide-react';
import InstallApp from './install-app';
import { desktop } from '../lib/platform';

type NavItem = { name: string; label: string; icon: ComponentType<{ size?: number }> };

const collapsedKey = 'vrcove-nav-collapsed';
function loadCollapsed(): string[] {
  try {
    const saved = JSON.parse(localStorage.getItem(collapsedKey) || '[]');
    return Array.isArray(saved) ? saved.filter((v) => typeof v === 'string') : [];
  } catch {
    return [];
  }
}

/** Main navigation. On phones it slides in when `open`. */
export default function Sidebar({ open }: { open: boolean }) {
  useLocale();
  const { section, sectionLink, saved, openPrivacy, coveAccount } = useApp();
  const staff = coveAccount?.role === 'admin' || coveAccount?.role === 'owner';
  // Groups can be folded to keep the sidebar short; the choice stays on this device.
  const [collapsed, setCollapsed] = useState(loadCollapsed);
  function toggle(label: string) {
    const next = collapsed.includes(label) ? collapsed.filter((l) => l !== label) : [...collapsed, label];
    setCollapsed(next);
    try {
      localStorage.setItem(collapsedKey, JSON.stringify(next));
    } catch {
      /* Folding still works until the page reloads. */
    }
  }
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
        {navigationGroups.map((group) => {
          const open = !collapsed.includes(group.label);
          // A folded group still shows the page you're on until you leave it.
          const shown = open ? group.items : group.items.filter((i) => i.name === section);
          return (
            <section className="nav-group" key={group.label} aria-label={tr(group.label)}>
              <h2 className="nav-label">
                <button aria-expanded={open} onClick={() => toggle(group.label)}>
                  {tr(group.label)}
                  <ChevronDown size={13} aria-hidden="true" />
                </button>
              </h2>
              {shown.map(item)}
            </section>
          );
        })}
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
