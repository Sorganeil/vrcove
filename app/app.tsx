import { useEffect, useRef, useState, type MouseEvent } from 'react';
import { X } from 'lucide-react';
import { api, expireVRChatSession, resetVRChatSession, useVRChatSession } from './lib/api';
import { t as tr, useLocale } from './lib/i18n';
import { useDesktopNotifications } from './lib/desktop-notifications';
import { desktop } from './lib/platform';
import { entityPath } from './lib/routes';
import { sectionPath, useRoute } from './lib/router';
import { AppContext, type AppContextValue, type AvatarBrowse } from './lib/app-context';
import { normalize, type Avatar } from './lib/avatar';
import type { CoveAccount, LinkedAccount, RawAvatar, Stats, VRChatUser } from './lib/types';
import { AvatarActionsProvider } from './components/avatar-actions';
import AvatarView from './components/avatar-view';
import Modal from './components/modal';
import PrivacyDialog from './components/privacy-dialog';
import SearchPalette from './components/search-palette';
import SessionNotices from './components/session-notices';
import Sidebar from './components/sidebar';
import SiteFooter from './components/site-footer';
import Topbar from './components/topbar';
import Login from './components/vrchat-login';
import DetailHost from './components/detail-host';
import { DetailView } from './components/detail-view';
import About from './pages/about';
import Companion from './pages/companion';
import DesktopLogger from './pages/desktop-logger';
import { FriendFeed, GameHistory, Screenshots } from './pages/desktop-history';
import Admin from './pages/admin';
import AccountPage from './pages/account';
import AvatarPage from './pages/avatar-page';
import Bookmarks from './pages/bookmarks';
import Settings from './pages/settings';
import SearchPage, { searchSections } from './pages/search';
import { CollectionPage } from './pages/collection';
import { CollectionsPage } from './pages/collections';
import { ProfilePage } from './pages/cove-profile';
import Dashboard from './pages/dashboard';
import Donations from './pages/donations';
import Favorites from './pages/favorites';
import Friends from './pages/friends';
import MyProfile from './pages/my-profile';
import MyUploads from './pages/my-uploads';
import Notifications from './pages/notifications';

const BOOKMARKS_KEY = 'atlas-bookmarks';

export default function App() {
  useLocale();
  const [user, setUser] = useState<VRChatUser | null>(null);
  const [coveAccount, setCoveAccount] = useState<CoveAccount | null>(null);
  // In the desktop app VRChat is signed in on the device itself.
  const [deviceLinks, setDeviceLinks] = useState<LinkedAccount[]>([]);
  const linkAccount = desktop ? { links: deviceLinks } : coveAccount;
  const refreshLinks = () =>
    desktop
      ? api('/account/device')
          .then((d) => setDeviceLinks(d.links))
          .catch(() => {})
      : api('/cove/me')
          .then(setCoveAccount)
          .catch(() => {});
  const [stats, setStats] = useState<Stats | null>(null);
  const [saved, setSaved] = useState<Avatar[]>([]);
  const [toast, setToast] = useState('');
  const [selected, setSelected] = useState<Avatar | null>(null);
  const [privacy, setPrivacy] = useState(false);
  const [palette, setPalette] = useState(false);
  const [mobile, setMobile] = useState(false);
  const [login, setLogin] = useState(false);
  const [loginTarget, setLoginTarget] = useState<string | undefined>();
  const [loginUsername, setLoginUsername] = useState('');
  const [switching, setSwitching] = useState(false);
  const connection = useVRChatSession();
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const { route, navigate: go } = useRoute((scroll) => {
    setMobile(false);
    if (scroll) window.scrollTo(0, 0);
  });
  const { section } = route;

  function navigate(next: string) {
    go(next);
    setMobile(false);
    window.scrollTo(0, 0);
  }
  // A real link to a section: crawlable and copyable, while a plain click switches without reloading.
  function sectionLink(next: string) {
    return {
      href: sectionPath(next),
      onClick: (e: MouseEvent) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
        e.preventDefault();
        navigate(next);
      },
    };
  }
  function notify(message: string) {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(''), 6000);
  }
  function accountChanged(me: CoveAccount | null) {
    setCoveAccount(me);
    if (desktop) return;
    if (!me || (user && !me.links.some((l) => l.vrchatId === user.id))) {
      resetVRChatSession();
      setUser(null);
    }
  }
  function requestConnection(expectedUserId?: string, username?: string) {
    if (!desktop) navigate('Account');
    if (!linkAccount) return;
    const target =
      expectedUserId === undefined
        ? connection.userId ||
          user?.id ||
          (linkAccount.links.length === 1 ? linkAccount.links[0].vrchatId : undefined)
        : expectedUserId;
    setLoginTarget(target);
    setLoginUsername(username || linkAccount.links.find((l) => l.vrchatId === target)?.username || '');
    setLogin(true);
  }
  function bookmark(a: Avatar) {
    const exists = saved.some((s) => s.id === a.id),
      next = exists ? saved.filter((s) => s.id !== a.id) : [a, ...saved].slice(0, 500);
    try {
      localStorage.setItem(BOOKMARKS_KEY, JSON.stringify(next));
      setSaved(next);
      notify(exists ? 'Removed from bookmarks' : 'Saved on this device');
    } catch {
      notify(tr('Your browser could not save this bookmark.'));
    }
  }
  // Opens avatar search with creator or tag filters in the URL, so the search starts fresh and is shareable.
  function browseAvatars(f: AvatarBrowse) {
    setSelected(null);
    const params = new URLSearchParams();
    if (f.author) {
      params.set('author', f.author.name);
      if (f.author.id) params.set('authorId', f.author.id);
    }
    if (f.tag) params.set('tag', f.tag);
    window.history.pushState(null, '', '/avatars?' + params);
    window.dispatchEvent(new PopStateEvent('popstate'));
    window.scrollTo(0, 0);
  }
  async function switchAccount(id: number) {
    setSwitching(true);
    try {
      const result = await api('/account/switch', { id });
      setSelected(null);
      setPalette(false);
      setUser(result.user);
      const account = await api('/cove/me').catch(() => null);
      if (account) setCoveAccount(account);
    } finally {
      setSwitching(false);
    }
  }

  // Bookmarks, catalog stats and the VRCove account load once.
  useEffect(() => {
    try {
      const d = JSON.parse(localStorage.getItem(BOOKMARKS_KEY) || '[]');
      if (Array.isArray(d))
        setSaved(d.filter((a) => a && typeof a.id === 'string' && typeof a.name === 'string').slice(0, 500));
    } catch {
      /* browser storage unavailable */
    }
    api('/stats')
      .then(setStats)
      .catch(() => {});
    api('/cove/me')
      .then(setCoveAccount)
      .catch(() => {});
    if (desktop) void refreshLinks();
  }, []);

  // Verify the VRChat session on entry, periodically while visible, and when returning to the tab.
  useEffect(() => {
    if (!linkAccount?.links?.length || login) return;
    let alive = true,
      checking = false,
      lastCheck = 0;
    const check = async () => {
      if (checking || document.visibilityState === 'hidden') return;
      checking = true;
      lastCheck = Date.now();
      try {
        const me = await api('/account/me');
        if (alive && me.id) setUser(me);
      } catch {
        /* Shared API handling reports confirmed session loss; temporary failures keep their own errors. */
      } finally {
        checking = false;
      }
    };
    void check();
    const interval = window.setInterval(() => void check(), 60_000);
    // Tab switches can fire focus and visibility events in bursts; at most one check per 30 seconds.
    const focus = () => {
      if (Date.now() - lastCheck >= 30_000) void check();
    };
    const switched = () => void check();
    window.addEventListener('vrcove:account-changed', switched);
    window.addEventListener('focus', focus);
    document.addEventListener('visibilitychange', focus);
    return () => {
      alive = false;
      window.clearInterval(interval);
      window.removeEventListener('vrcove:account-changed', switched);
      window.removeEventListener('focus', focus);
      document.removeEventListener('visibilitychange', focus);
    };
  }, [coveAccount?.username, linkAccount?.links?.length, login]);

  // Announce expiry when the session's lifetime runs out, even without an action.
  useEffect(() => {
    if (!connection.expiresAt) return;
    const expire = () => expireVRChatSession();
    const timer = window.setTimeout(expire, Math.max(0, connection.expiresAt - Date.now()));
    const onVisible = () => {
      if (connection.expiresAt! <= Date.now()) expire();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [connection.expiresAt]);

  // A lost session or a different account closes anything that acted for the previous one.
  const [sessionKey, setSessionKey] = useState({ userId: user?.id, issue: connection.issue });
  if (sessionKey.userId !== user?.id || sessionKey.issue !== connection.issue) {
    setSessionKey({ userId: user?.id, issue: connection.issue });
    setSelected(null);
    setPalette(false);
    if (connection.issue && user) setUser(null);
  }

  // Avatar quick view requested elsewhere (collections, reviews): open the modal, or the full page if not found.
  useEffect(() => {
    const open = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      api('/catalog?q=' + encodeURIComponent(id))
        .then(async (d) => {
          const a = d.avatars[0] || (user ? normalize(await api('/account/avatar?id=' + id)) : null);
          if (a) setSelected(a);
          else window.location.assign(entityPath('avatar', id));
        })
        .catch(() => window.location.assign(entityPath('avatar', id)));
    };
    window.addEventListener('vrcove:avatar', open);
    return () => window.removeEventListener('vrcove:avatar', open);
  }, [user]);

  // Ctrl/Cmd+K opens search; / jumps to avatar search.
  const navigateRef = useRef(navigate);
  useEffect(() => {
    navigateRef.current = navigate;
  });
  useEffect(() => {
    const fn = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPalette(true);
        return;
      }
      if (
        e.key === '/' &&
        !['INPUT', 'TEXTAREA', 'SELECT'].includes((e.target as HTMLElement).tagName) &&
        !document.querySelector('[role="dialog"]')
      ) {
        e.preventDefault();
        navigateRef.current('Explore');
        setTimeout(() => document.querySelector<HTMLInputElement>('.search-input input')?.focus(), 0);
      }
    };
    document.addEventListener('keydown', fn);
    return () => document.removeEventListener('keydown', fn);
  }, []);

  const app: AppContextValue = {
    user,
    setUser,
    coveAccount,
    setCoveAccount,
    linkAccount,
    accountChanged,
    stats,
    section,
    navigate,
    sectionLink,
    notify,
    requestConnection,
    saved,
    setSaved,
    bookmark,
    openAvatar: setSelected,
    closeAvatar: () => setSelected(null),
    openPrivacy: () => setPrivacy(true),
    browseAvatars,
  };
  const connect = () => requestConnection();
  const vrchatKey = user?.id || 'guest';
  useDesktopNotifications(user?.id);

  return (
    <AppContext.Provider value={app}>
      <AvatarActionsProvider>
        <div className="app-shell">
          {mobile && (
            <button className="scrim" aria-label={tr('Close navigation')} onClick={() => setMobile(false)} />
          )}
          <Sidebar open={mobile} />
          <div className="main-shell">
            <Topbar onMenu={() => setMobile(true)} onSearch={() => setPalette(true)} />
            <main>
              <SessionNotices onSwitch={switchAccount} switching={switching} connecting={login} />
              {section === 'Home' && (
                <Dashboard
                  key={vrchatKey}
                  coveAccount={linkAccount}
                  user={user}
                  onLogin={connect}
                  navigate={navigate}
                  stats={stats}
                  notify={notify}
                  onUser={setUser}
                  onAvatar={setSelected}
                />
              )}
              {searchSections.includes(section) && <SearchPage key={route.version} section={section} />}
              {section === 'Settings' && <Settings notify={notify} saved={saved} setSaved={setSaved} />}
              {section === 'Bookmarks' && <Bookmarks />}
              {section === 'Favorites' && <Favorites key={vrchatKey} />}
              {section === 'Friends' && <Friends key={vrchatKey} />}
              {section === 'Notifications' && <Notifications key={vrchatKey} />}
              {section === 'Profile' && <MyProfile key={vrchatKey} />}
              {section === 'Donations' && <Donations />}
              {section === 'About' && (
                <About onPrivacy={() => setPrivacy(true)} onDonate={() => navigate('Donations')} />
              )}
              {section === 'Companion' && (desktop ? <DesktopLogger /> : <Companion />)}
              {section === 'History' && <GameHistory key={route.version} />}
              {section === 'Feed' && <FriendFeed />}
              {section === 'Screenshots' && <Screenshots />}
              {section === 'Admin' && <Admin key={route.version} notify={notify} />}
              {section === 'Collections' && <CollectionsPage notify={notify} />}
              {section === 'CoveProfile' && (
                <ProfilePage key={route.coveTarget} username={route.coveTarget} />
              )}
              {section === 'Collection' && (
                <CollectionPage key={route.coveTarget} id={route.coveTarget} notify={notify} />
              )}
              {section === 'Account' && (
                <AccountPage
                  onSwitch={switchAccount}
                  onAccountChange={accountChanged}
                  onDisconnected={() => {
                    resetVRChatSession();
                    setUser(null);
                  }}
                  user={user}
                  configured={stats?.accountsConfigured}
                  onLogin={requestConnection}
                  notify={notify}
                />
              )}
              {section === 'Uploads' && (
                <MyUploads
                  key={vrchatKey}
                  coveAccount={linkAccount}
                  user={user}
                  onLogin={connect}
                  onAvatar={(a: RawAvatar) => setSelected(normalize(a))}
                />
              )}
              {section === 'Avatar' && <AvatarPage key={route.avatarId} id={route.avatarId} />}
              {route.detail && (
                <DetailView
                  key={route.detail.kind + route.detail.id + vrchatKey}
                  coveAccount={linkAccount}
                  kind={route.detail.kind}
                  id={route.detail.id}
                  user={user}
                  onLogin={connect}
                  notify={notify}
                  mode="page"
                  onBack={() => window.history.back()}
                />
              )}
              <SiteFooter />
            </main>
          </div>
          {selected && (
            <Modal title={selected.name} close={() => setSelected(null)} wide>
              <AvatarView avatar={selected} page={false} />
            </Modal>
          )}
          {login && linkAccount && (
            <Login
              initialUsername={loginUsername}
              expectedUserId={loginTarget}
              configured={stats?.loginConfigured}
              close={() => setLogin(false)}
              connected={(u: VRChatUser, similar: boolean) => {
                setUser(u);
                setLogin(false);
                void refreshLinks();
                notify(
                  desktop
                    ? 'Signed in to VRChat on this device'
                    : similar
                      ? 'VRChat account linked. Your VRCove name resembles your VRChat name; use an unrelated name to reduce correlation.'
                      : 'VRChat account linked',
                );
              }}
            />
          )}
          {privacy && <PrivacyDialog close={() => setPrivacy(false)} />}
          {palette && (
            <SearchPalette
              coveAccount={linkAccount}
              close={() => setPalette(false)}
              user={user}
              onLogin={connect}
              onAvatar={(a: Avatar) => setSelected(a)}
            />
          )}
          <DetailHost
            coveAccount={linkAccount}
            key={(connection.issue || 'connected') + (user?.id || '')}
            user={user}
            onLogin={connect}
            notify={notify}
          />
          {toast && (
            <div className="toast" role="status">
              <span>{toast}</span>
              <button aria-label={tr('Dismiss')} onClick={() => setToast('')}>
                <X size={15} />
              </button>
            </div>
          )}
        </div>
      </AvatarActionsProvider>
    </AppContext.Provider>
  );
}
