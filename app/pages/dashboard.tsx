import { desktop } from '../lib/platform';
import { gateTitle } from '../components/vrchat-gate';
import UserStatus, { notInGame, userPresence } from '../components/user-status';
import CompanionDownload from '../components/companion-download';
import { api, useVRChatSession } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect } from 'react';
import { detailLink } from '../components/detail-links';
import {
  Users,
  Globe2,
  Heart,
  UserRound,
  ShieldCheck,
  ArrowRight,
  Search,
  RefreshCw,
  Bell,
  Check,
  Settings,
} from 'lucide-react';
function Photo({ url, alt = '' }: any) {
  useLocale();
  const [fail, setFail] = useState(false);
  useEffect(() => setFail(false), [url]);
  return url && !fail ? (
    <img
      src={'/api/image?url=' + encodeURIComponent(url)}
      alt={alt}
      onError={() => setFail(true)}
      loading="lazy"
    />
  ) : (
    <span className="dash-photo-fallback">
      <UserRound size={24} />
    </span>
  );
}
export default function Dashboard({
  coveAccount,
  user,
  onLogin,
  navigate,
  stats,
  notify,
  onUser,
  onAvatar,
}: any) {
  useLocale();
  const connection = useVRChatSession();
  const [friends, setFriends] = useState<any[]>([]),
    [worlds, setWorlds] = useState<any[]>([]),
    [notices, setNotices] = useState<any[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [status, setStatus] = useState(user?.status || 'active'),
    [statusText, setStatusText] = useState(user?.statusDescription || ''),
    [previews, setPreviews] = useState<any[]>([]),
    [previewError, setPreviewError] = useState('');
  async function refresh() {
    if (!user) return;
    setBusy(true);
    setError('');
    const results = await Promise.allSettled([
      api('/account/friends'),
      api('/account/worlds'),
      api('/account/notifications'),
    ]);
    if (results[0].status === 'fulfilled') setFriends(results[0].value);
    if (results[1].status === 'fulfilled') setWorlds(results[1].value);
    if (results[2].status === 'fulfilled') setNotices(results[2].value);
    if (results.some((r) => r.status === 'rejected'))
      setError(tr('Some VRChat panels could not be refreshed. Your session may have expired.'));
    setBusy(false);
  }
  useEffect(() => {
    refresh();
  }, [user?.id]);
  useEffect(() => {
    setStatus(user?.status || 'active');
    setStatusText(user?.statusDescription || '');
  }, [user?.status, user?.statusDescription]);
  useEffect(() => {
    let alive = true;
    api('/catalog?sort=newest')
      .then((result) => {
        if (alive) setPreviews(result.avatars.slice(0, 6));
      })
      .catch(() => {
        if (alive) setPreviewError(tr('Avatar previews could not be loaded.'));
      });
    return () => {
      alive = false;
    };
  }, []);
  async function saveStatus() {
    setBusy(true);
    try {
      const updated = await api('/account/profile', { status, statusDescription: statusText });
      onUser(updated);
      notify(tr('VRChat status updated'));
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  // People in game come first; those on the website or app follow with a "Not in game" badge.
  const inGame = friends.filter((f) => !notInGame(f)),
    elsewhere = friends.filter((f) => notInGame(f));
  const friendCard = (f: any) => (
    <a key={f.id} {...detailLink('user', f.id)}>
      <div>
        <Photo url={f.userIcon || f.profilePicOverride || f.currentAvatarThumbnailImageUrl} />
        <i className={'status-dot ' + userPresence(f).tone} aria-hidden="true" />
      </div>
      <strong>{f.displayName}</strong>
      <UserStatus user={f} />
    </a>
  );
  if (!user)
    return (
      <div className="guest-home">
        <div className="page-title">
          <div>
            <h1>{tr('Home')}</h1>
            <p>
              {tr(
                desktop ? 'Your VRChat account, on your desktop.' : 'Your VRChat account, from your browser.',
              )}
            </p>
          </div>
        </div>
        <div className="privacy-at-a-glance">
          <ShieldCheck size={17} />
          <div>
            <strong>{tr('No ads. No analytics.')}</strong>
            <span>{tr('No Amplitude, Sentry or tracking pixels in VRCove.')}</span>
          </div>
          <button onClick={() => navigate('Settings')}>
            {tr('Why VRCove?')}
            <ArrowRight size={13} />
          </button>
        </div>
        <section className="guest-account">
          <div className="guest-account-mark">
            <ShieldCheck size={28} />
          </div>
          <div className="guest-copy">
            <h2>{tr(desktop ? 'Sign in to VRChat' : 'Sign in to VRCove')}</h2>
            <p>{tr('Catch up with friends, find a world, or change your avatar.')}</p>
            <span>
              {tr(
                desktop
                  ? 'This app talks to VRChat directly. Your password and session never pass through VRCove.'
                  : 'Link VRChat after signing in to VRCove.',
              )}
            </span>
          </div>
          <div className="guest-actions">
            <button className="primary" onClick={onLogin}>
              {tr(desktop ? 'Sign in to VRChat' : 'Connect account')}
              <ArrowRight size={16} />
            </button>
            <button className="text-link" onClick={() => navigate('Settings')}>
              {tr('How your data is handled')}
            </button>
          </div>
        </section>
        <div className="guest-intro">
          <section className="preview-section">
            <div className="preview-heading">
              <div>
                <h2>{tr('Recently updated avatars')}</h2>
                <p>
                  {stats?.indexed
                    ? tr('{count} public avatars in the catalog.', { count: stats.indexed })
                    : tr('Browse the public avatar catalog.')}
                </p>
              </div>
              <button className="text-link" onClick={() => navigate('Explore')}>
                {tr('Browse all')}
                <ArrowRight size={14} />
              </button>
            </div>
            {previewError ? (
              <div className="error-banner">{previewError}</div>
            ) : (
              <div className="home-avatar-grid">
                {previews.length
                  ? previews.map((a) => (
                      <button className="home-avatar-card" key={a.id} onClick={() => onAvatar(a)}>
                        <span className="home-avatar-image">
                          <Photo url={a.thumbnail} alt={a.name} />
                          <span className="home-avatar-platform">
                            {a.pc ? 'PC' : ''}
                            {a.pc && a.quest ? ' · ' : ''}
                            {a.quest ? 'Quest' : ''}
                          </span>
                        </span>
                        <span className="home-avatar-info">
                          <strong className="home-avatar-name">{a.name}</strong>
                          <span className="home-avatar-author">{a.author}</span>
                        </span>
                      </button>
                    ))
                  : Array.from({ length: 6 }, (_, i) => (
                      <div className="skeleton-card" key={i}>
                        <div />
                        <span />
                        <small />
                      </div>
                    ))}
              </div>
            )}
          </section>
          <aside className="guest-side-notes">
            <section>
              <h2>{tr('Your account')}</h2>
              <p>
                {tr(
                  desktop
                    ? 'Sign in to VRChat to use account actions. A VRCove account is optional.'
                    : 'Create a VRCove account, then link VRChat to use account actions.',
                )}
              </p>
              {[
                { name: 'Friends', icon: Users, detail: 'Online friends and profiles' },
                { name: 'Worlds', icon: Globe2, detail: 'Live public world listings' },
                { name: 'Favorites', icon: Heart, detail: 'Your saved avatars and worlds' },
                { name: 'MyGroups', label: 'My groups', icon: Users, detail: 'Your communities' },
                { name: 'Notifications', icon: Bell, detail: 'Invites and friend requests' },
              ].map((n) => (
                <button key={n.name} onClick={() => navigate(n.name)}>
                  <n.icon size={18} />
                  <span>
                    <strong>{tr((n as { label?: string }).label || n.name)}</strong>
                    <small>{tr(n.detail)}</small>
                  </span>
                  <ArrowRight size={14} />
                </button>
              ))}
            </section>
            <section className="guest-privacy">
              <ShieldCheck size={18} />
              <div>
                <h3>{tr('Private by default')}</h3>
                <p>
                  {tr(
                    'No Amplitude, Sentry, ad pixels or session replay. Bookmarks and notes stay on this device. Account requests still go to VRChat.',
                  )}
                </p>
                <button className="text-link" onClick={() => navigate('Settings')}>
                  {tr('Privacy settings')}
                  <ArrowRight size={13} />
                </button>
              </div>
            </section>
          </aside>
        </div>
        <CompanionDownload />
      </div>
    );
  return (
    <div className="dashboard">
      <div className="page-title">
        <div>
          <h1>{tr('Home')}</h1>
          <p>{tr('Welcome back, {name}.', { name: user.displayName })}</p>
        </div>
        <button className="standard-button" disabled={busy} onClick={refresh}>
          <RefreshCw size={14} className={busy ? 'spin' : ''} />
          {tr('Refresh')}
        </button>
      </div>
      {!user && (
        <section className="connection-banner">
          <div className="connection-symbol">
            <ShieldCheck size={32} />
          </div>
          <div>
            <h2>{gateTitle(coveAccount, connection.issue)}</h2>
            <p>
              {tr('See friends, manage your profile, explore worlds, and change avatars.')}
              <br />
              {tr('Your password is never saved. Your session is encrypted in your browser cookie.')}
            </p>
          </div>
          <button className="primary" onClick={onLogin}>
            {tr('Connect VRChat')}
            <ArrowRight size={16} />
          </button>
        </section>
      )}
      {error && <div className="error-banner">{error}</div>}
      <CompanionDownload />
      <div className="dashboard-grid">
        <div className="dashboard-main">
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>
                <Users size={17} />
                {tr('Friends online')}
                {user && <small>{friends.length === 100 ? '100+' : friends.length}</small>}
              </h2>
              <button onClick={() => navigate('Friends')}>
                {tr('View all')}
                <ArrowRight size={14} />
              </button>
            </div>
            {user ? (
              <div className="online-grid">
                {[...inGame, ...elsewhere].slice(0, 12).map(friendCard)}
                {!friends.length && (
                  <div className="panel-empty">
                    <Users size={25} />
                    <p>{busy ? tr('Loading friends…') : tr('No online friends to show.')}</p>
                  </div>
                )}
              </div>
            ) : (
              <div className="panel-empty">
                <Users size={27} />
                <p>{tr('Connect to see who’s online.')}</p>
                <button onClick={onLogin}>
                  {tr('Connect account')}
                  <ArrowRight size={13} />
                </button>
              </div>
            )}
          </section>
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>
                <Globe2 size={17} />
                {tr('Worlds to explore')}
              </h2>
              <button onClick={() => navigate('Worlds')}>
                {tr('Browse worlds')}
                <ArrowRight size={14} />
              </button>
            </div>
            {user ? (
              <div className="dashboard-worlds">
                {worlds.slice(0, 6).map((w) => (
                  <a key={w.id} {...detailLink('world', w.id)}>
                    <div>
                      <Photo url={w.thumbnailImageUrl || w.imageUrl} />
                      <span>
                        <Users size={10} />
                        {w.occupants ?? '?'}
                      </span>
                    </div>
                    <strong>{w.name}</strong>
                    <small>{w.authorName}</small>
                  </a>
                ))}
              </div>
            ) : (
              <div className="panel-empty">
                <Globe2 size={27} />
                <p>{tr('Live world listings are available after you connect.')}</p>
              </div>
            )}
          </section>
          <section className="dashboard-panel catalog-callout">
            <div>
              <Search size={25} />
              <div>
                <h2>{tr('Avatar search')}</h2>
                <p>
                  {tr('Search {count} public avatars by name, creator, platform, or performance.', {
                    count: stats?.indexed || 0,
                  })}
                </p>
              </div>
            </div>
            <button className="standard-button" onClick={() => navigate('Explore')}>
              {tr('Browse avatars')}
              <ArrowRight size={14} />
            </button>
          </section>
        </div>
        <aside className="dashboard-aside">
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>
                <UserRound size={17} />
                {tr('My status')}
              </h2>
            </div>
            {user ? (
              <div className="status-editor">
                <div className="dash-self">
                  <Photo url={user.userIcon || user.currentAvatarThumbnailImageUrl} />
                  <div>
                    <strong>{user.displayName}</strong>
                    <small>
                      {user.location === 'offline'
                        ? tr('Offline')
                        : user.location === 'private'
                          ? 'Private instance'
                          : user.state || 'Connected'}
                    </small>
                  </div>
                </div>
                <label>
                  {tr('Status')}
                  <select value={status} onChange={(e) => setStatus(e.target.value)}>
                    <option value="active">{tr('Online')}</option>
                    <option value="join me">{tr('Join me')}</option>
                    <option value="ask me">{tr('Ask me')}</option>
                    <option value="busy">{tr('Do not disturb')}</option>
                  </select>
                </label>
                <label>
                  {tr('Status message')}
                  <input
                    value={statusText}
                    onChange={(e) => setStatusText(e.target.value)}
                    maxLength={32}
                    placeholder={tr('What are you up to?')}
                  />
                </label>
                <button className="primary" onClick={saveStatus} disabled={busy}>
                  <Check size={14} />
                  {tr('Save status')}
                </button>
              </div>
            ) : (
              <div className="panel-empty compact">
                <UserRound size={25} />
                <p>{tr('Not connected')}</p>
                <button onClick={onLogin}>{tr('Sign in to VRChat')}</button>
              </div>
            )}
          </section>
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>
                <Bell size={17} />
                {tr('Notifications')}
              </h2>
              <button onClick={() => navigate('Notifications')}>
                {tr('All')}
                <ArrowRight size={13} />
              </button>
            </div>
            {user && notices.length ? (
              <div className="dash-notifications">
                {notices.slice(0, 4).map((n) => (
                  <button key={n.id} onClick={() => navigate('Notifications')}>
                    <Bell size={15} />
                    <div>
                      <strong>{n.senderUsername || 'VRChat'}</strong>
                      <p>{n.message || n.type}</p>
                    </div>
                  </button>
                ))}
              </div>
            ) : (
              <div className="panel-empty compact">
                <Bell size={23} />
                <p>{user ? tr('No recent notifications') : tr('Connect to view notifications.')}</p>
              </div>
            )}
          </section>
          <section className="dashboard-panel privacy-summary">
            <h2>
              <ShieldCheck size={17} />
              {tr('Privacy controls')}
            </h2>
            <ul>
              <li>
                <Check size={12} />
                {tr('No Amplitude, Sentry or ad scripts')}
              </li>
              <li>
                <Check size={12} />
                {tr('Encrypted VRChat session cookie')}
              </li>
              <li>
                <Check size={12} />
                {tr('Images proxied through this site')}
              </li>
              <li>
                <Check size={12} />
                {tr('No server-side activity history')}
              </li>
            </ul>
            <button onClick={() => navigate('Settings')}>
              {tr('Manage local data')}
              <Settings size={13} />
            </button>
          </section>
        </aside>
      </div>
    </div>
  );
}
