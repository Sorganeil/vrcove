import VRChatGate from '../components/vrchat-gate';
import { api } from '../lib/api';
import LanguagePicker from '../components/language-picker';
import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect, useRef } from 'react';
import PrivacyApproach from '../components/privacy-approach';
import { detailLink } from '../components/detail-links';
import { Photo } from '../components/photo';
import {
  Search,
  Users,
  ShieldCheck,
  ArrowUpRight,
  UserRound,
  Download,
  Upload,
  Trash2,
  LoaderCircle,
  Check,
  ExternalLink,
  X,
} from 'lucide-react';
function Image({ src }: any) {
  useLocale();
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);
  return src && !failed ? (
    <img src={'/api/image?url=' + encodeURIComponent(src)} alt="" onError={() => setFailed(true)} />
  ) : (
    <div className="dash-photo-fallback">
      <UserRound size={24} />
    </div>
  );
}
export default function BrowserTools({
  coveAccount,
  section,
  mine = false,
  onAvatar,
  user,
  onLogin,
  notify,
  saved,
  setSaved,
}: any) {
  useLocale();
  const [type, setType] = useState(
      section === 'Users' ? 'users' : section === 'Groups' ? 'groups' : 'avatars',
    ),
    [q, setQ] = useState(''),
    [rows, setRows] = useState<any[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [offset, setOffset] = useState(0),
    [confirm, setConfirm] = useState<any>(null),
    [notes, setNotes] = useState('');
  const [groupScope, setGroupScope] = useState<'mine' | 'all'>(mine ? 'mine' : 'all');
  const [submitted, setSubmitted] = useState<string | null>(null),
    request = useRef(0);
  useEffect(
    () => () => {
      request.current++;
    },
    [],
  );
  useEffect(() => {
    try {
      setNotes(localStorage.getItem('vrcove-notes') ?? localStorage.getItem('veilchat-notes') ?? '');
    } catch {
      /* browser storage unavailable */
    }
  }, []);
  useEffect(() => {
    setRows([]);
    setError('');
    setOffset(0);
    if (section === 'Users') setType('users');
    if (section === 'Groups' && user) {
      if (groupScope === 'mine') void loadGroups();
      else void search({ preventDefault() {} }, 0, 'all');
    }
  }, [section, user?.id]);
  async function loadGroups() {
    const current = ++request.current;
    setBusy(true);
    setError('');
    setSubmitted(null);
    setOffset(0);
    try {
      const result = await api('/account/groups');
      if (current === request.current) setRows(result);
    } catch (e) {
      if (current === request.current) setError((e as Error).message);
    } finally {
      if (current === request.current) setBusy(false);
    }
  }
  async function search(e?: any, nextOffset = 0, scope = groupScope) {
    e?.preventDefault();
    if (type !== 'avatars' && !user) {
      onLogin();
      return;
    }
    const term = e ? q.trim() : (submitted ?? q.trim());
    if (section === 'Groups' && scope === 'mine') {
      setSubmitted(term || null);
      setOffset(0);
      return;
    }
    if (!term) {
      if (section === 'Groups') {
        request.current++;
        setRows([]);
        setSubmitted(null);
        setOffset(0);
        setError('');
        setBusy(false);
      }
      return;
    }
    const current = ++request.current;
    setBusy(true);
    setError('');
    setOffset(nextOffset);
    setSubmitted(term);
    try {
      const result =
        type === 'avatars'
          ? await api('/catalog?' + new URLSearchParams({ q: term, page: String(nextOffset / 36) }))
          : await api(
              '/account/search?' + new URLSearchParams({ type, q: term, offset: String(nextOffset) }),
            );
      if (current === request.current)
        setRows(
          type === 'avatars' ? result.avatars || [] : Array.isArray(result) ? result : result.results || [],
        );
    } catch (e) {
      if (current === request.current) setError((e as Error).message);
    } finally {
      if (current === request.current) setBusy(false);
    }
  }
  function clearSearch() {
    request.current++;
    setQ('');
    setSubmitted(null);
    setRows([]);
    setOffset(0);
    setError('');
    setBusy(false);
    if (section === 'Groups' && groupScope === 'mine' && user) loadGroups();
  }
  function changeGroupScope(scope: 'mine' | 'all') {
    if (scope === groupScope) return;
    request.current++;
    setGroupScope(scope);
    setRows([]);
    setSubmitted(null);
    setOffset(0);
    setError('');
    setBusy(false);
    if (!user) return;
    if (scope === 'mine') void loadGroups();
    else void search({ preventDefault() {} }, 0, scope);
  }
  const visibleRows =
    section === 'Groups' && groupScope === 'mine'
      ? rows.filter((row) =>
          [row.name, row.shortCode, row.discriminator]
            .filter(Boolean)
            .join(' ')
            .toLocaleLowerCase()
            .includes(q.trim().toLocaleLowerCase()),
        )
      : rows;

  async function action() {
    if (!confirm) return;
    setBusy(true);
    try {
      await api('/account/action', { action: confirm.action, id: confirm.id });
      notify(tr('VRChat confirmed the action.'));
      setConfirm(null);
      if (section === 'Groups') loadGroups();
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function exportData() {
    const blob = new Blob([JSON.stringify({ version: 1, bookmarks: saved, notes }, null, 2)], {
        type: 'application/json',
      }),
      url = URL.createObjectURL(blob),
      a = document.createElement('a');
    a.href = url;
    a.download = 'vrcove-local-data.json';
    a.click();
    URL.revokeObjectURL(url);
  }
  async function importData(e: any) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      if (file.size > 5000000) throw new Error('Choose a backup smaller than 5 MB.');
      const backup = JSON.parse(await file.text());
      if (backup.version !== 1 || !Array.isArray(backup.bookmarks) || backup.bookmarks.length > 500)
        throw new Error('This is not a supported VRCove backup.');
      const merged = [...saved];
      for (const a of backup.bookmarks) {
        if (
          !/^avtr_[a-f0-9-]{36}$/.test(a.id) ||
          typeof a.name !== 'string' ||
          typeof a.author !== 'string' ||
          !Array.isArray(a.tags)
        )
          throw new Error('Invalid bookmark in backup.');
        if (!merged.some((b) => b.id === a.id)) merged.push(a);
      }
      const next = merged.slice(0, 500);
      localStorage.setItem('atlas-bookmarks', JSON.stringify(next));
      setSaved(next);
      if (typeof backup.notes === 'string') {
        localStorage.setItem('vrcove-notes', backup.notes.slice(0, 100000));
        localStorage.removeItem('veilchat-notes');
        setNotes(backup.notes.slice(0, 100000));
      }
      notify(tr('Local data imported.'));
    } catch (e) {
      notify((e as Error).message);
    }
    e.target.value = '';
  }
  if (section === 'Settings')
    return (
      <div className="settings-page">
        <div className="page-title">
          <div>
            <h1>{tr('Privacy & settings')}</h1>
            <p>{tr('Control what VRCove keeps on this device.')}</p>
          </div>
          <ShieldCheck size={25} />
        </div>
        <section className="dashboard-panel language-settings">
          <LanguagePicker />
        </section>
        <PrivacyApproach />
        <div className="settings-grid">
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>{tr('Privacy defaults')}</h2>
            </div>
            <div className="settings-content">
              <div className="setting-row">
                <div>
                  <strong>{tr('Activity tracking')}</strong>
                  <p>{tr('No friend-location history or play-time tracking is collected.')}</p>
                </div>
                <span className="setting-state">{tr('Off')}</span>
              </div>
              <div className="setting-row">
                <div>
                  <strong>{tr('Automatic refresh')}</strong>
                  <p>{tr('Account data loads when you open a section or press Refresh.')}</p>
                </div>
                <span className="setting-state">{tr('Manual')}</span>
              </div>
              <div className="setting-row">
                <div>
                  <strong>{tr('Images')}</strong>
                  <p>{tr('Proxied through VRCove. No direct image-host requests from your browser.')}</p>
                </div>
                <Check size={16} />
              </div>
              <div className="setting-row">
                <div>
                  <strong>{tr('Session persistence')}</strong>
                  <p>
                    {tr(
                      'Encrypted, HttpOnly cookie. Maximum 2 weeks. No VRChat credentials in the database.',
                    )}
                  </p>
                </div>
                <Check size={16} />
              </div>
            </div>
          </section>
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>{tr('Local data')}</h2>
            </div>
            <div className="settings-content">
              <p>
                {saved.length}{' '}
                {tr('bookmarks saved in this browser. Export a backup before clearing browser data.')}
              </p>
              <div className="settings-actions">
                <button className="standard-button" onClick={exportData}>
                  <Download size={14} />
                  {tr('Export backup')}
                </button>
                <label className="standard-button file-label">
                  <Upload size={14} />
                  {tr('Import backup')}
                  <input
                    aria-label={tr('Import local backup')}
                    type="file"
                    accept="application/json"
                    onChange={importData}
                  />
                </label>
                <button
                  className="standard-button danger-text"
                  onClick={() =>
                    setConfirm({
                      action: 'clearLocal',
                      text: 'Clear all local bookmarks and notes? This cannot be undone without a backup.',
                    })
                  }
                >
                  <Trash2 size={14} />
                  {tr('Clear local data')}
                </button>
              </div>
            </div>
          </section>
          <section className="dashboard-panel notes-panel">
            <div className="panel-title">
              <h2>{tr('Private notes')}</h2>
              <small>{tr('Stored on this device only')}</small>
            </div>
            <div className="settings-content">
              <textarea
                aria-label={tr('Private notes')}
                value={notes}
                onChange={(e) => {
                  setNotes(e.target.value);
                  try {
                    localStorage.setItem('vrcove-notes', e.target.value);
                    localStorage.removeItem('veilchat-notes');
                  } catch {
                    notify(tr('Your browser could not save these notes.'));
                  }
                }}
                maxLength={100000}
                placeholder={tr('Worlds to visit, avatar ideas, people to catch up with…')}
              />
            </div>
          </section>
          <section className="dashboard-panel">
            <div className="panel-title">
              <h2>{tr('Browser-only limits')}</h2>
            </div>
            <div className="settings-content">
              <p>
                {tr(
                  'VRCove cannot read VRChat game logs, track time while closed, run overlays, control Discord, or manage game files. The optional desktop companion doesn’t add any of this either: it only finds avatar IDs for the shared catalog.',
                )}
              </p>
              <p>
                {tr(
                  'Payments, identity verification, account recovery, marketplace purchases, and advanced group administration use VRChat’s official website.',
                )}
              </p>
              <a className="text-link" href="https://vrchat.com/home" target="_blank" rel="noreferrer">
                {tr('Open VRChat account settings')}
                <ExternalLink size={13} />
              </a>
            </div>
          </section>
        </div>
        {confirm && (
          <div className="modal-backdrop">
            <div
              className="modal confirm-content"
              role="dialog"
              aria-modal="true"
              aria-label={tr('Clear local data')}
            >
              <h2>{tr('Clear local data?')}</h2>
              <p>{confirm.text}</p>
              <button
                className="primary"
                onClick={() => {
                  localStorage.removeItem('atlas-bookmarks');
                  localStorage.removeItem('vrcove-notes');
                  localStorage.removeItem('veilchat-notes');
                  setSaved([]);
                  setNotes('');
                  setConfirm(null);
                  notify(tr('Local data cleared.'));
                }}
              >
                {tr('Clear bookmarks and notes')}
              </button>
              <button onClick={() => setConfirm(null)}>{tr('Cancel')}</button>
            </div>
          </div>
        )}
      </div>
    );
  return (
    <div className="browser-tools">
      <div className="page-title">
        <div>
          <h1>
            {section === 'Groups'
              ? tr(mine ? 'My groups' : 'Groups')
              : section === 'Users'
                ? tr('Users')
                : tr('Search VRChat')}
          </h1>
          <p>
            {section === 'Groups'
              ? tr(mine ? 'Groups you belong to.' : 'Discover communities across VRChat.')
              : section === 'Users'
                ? tr('Find VRChat people by name.')
                : tr('Find avatars, people, worlds, and groups.')}
          </p>
        </div>
        {section === 'Groups' && (
          <button
            className="standard-button"
            onClick={() => (groupScope === 'mine' ? loadGroups() : search({ preventDefault() {} }))}
            disabled={!user || busy || (groupScope === 'all' && !q.trim())}
          >
            {tr('Refresh')}
          </button>
        )}
      </div>
      {['Search', 'Users', 'Groups'].includes(section) && (
        <>
          {section === 'Groups' && !mine && (
            <div className="group-search-scope">
              <div className="chips search-types" role="group" aria-label={tr('Group search scope')}>
                {(
                  [
                    ['mine', 'My groups'],
                    ['all', 'All groups'],
                  ] as const
                ).map(([value, label]) => (
                  <button
                    key={value}
                    type="button"
                    className={groupScope === value ? 'chosen' : ''}
                    aria-pressed={groupScope === value}
                    onClick={() => changeGroupScope(value)}
                  >
                    {tr(label)}
                  </button>
                ))}
              </div>
              <p className="muted">
                {tr(
                  groupScope === 'mine'
                    ? 'Search only the groups you belong to.'
                    : 'Search all VRChat groups to discover a community.',
                )}
              </p>
            </div>
          )}
          {section === 'Search' && (
            <div className="chips search-types">
              {[
                ['avatars', 'Avatars'],
                ['users', 'People'],
                ['worlds', 'Worlds'],
                ['groups', 'Groups'],
              ].map(([v, l]) => (
                <button
                  key={v}
                  className={type === v ? 'chosen' : ''}
                  onClick={() => {
                    request.current++;
                    setType(v);
                    setQ('');
                    setSubmitted(null);
                    setRows([]);
                    setOffset(0);
                    setError('');
                    setBusy(false);
                  }}
                >
                  {tr(l)}
                </button>
              ))}
            </div>
          )}
          {(user || type === 'avatars') && (
            <form className="search-row" onSubmit={search}>
              <div className="search-input">
                <Search size={18} />
                <input
                  type="search"
                  aria-label={tr(
                    section === 'Users'
                      ? 'Search users'
                      : section === 'Groups'
                        ? 'Search groups'
                        : 'Search VRChat',
                  )}
                  placeholder={
                    section === 'Groups'
                      ? tr(groupScope === 'mine' ? 'Search my groups…' : 'Search all groups…')
                      : tr('Search {type}…', {
                          type: tr(
                            { users: 'People', avatars: 'Avatars', worlds: 'Worlds', groups: 'Groups' }[
                              type
                            ] || type,
                          ),
                        })
                  }
                  value={q}
                  onChange={(e) => {
                    if (!e.target.value) clearSearch();
                    else setQ(e.target.value);
                  }}
                  required
                  maxLength={100}
                />
                {q && (
                  <button type="button" aria-label={tr('Clear search')} onClick={clearSearch}>
                    <X size={16} />
                  </button>
                )}
              </div>
              <button className="primary" disabled={busy}>
                {tr('Search')}
              </button>
            </form>
          )}
        </>
      )}
      {!user && !(section === 'Search' && type === 'avatars') ? (
        <VRChatGate
          coveAccount={coveAccount}
          section={
            section === 'Groups'
              ? tr(mine ? 'My groups' : 'Groups')
              : section === 'Users'
                ? tr('Users')
                : tr({ users: 'People', worlds: 'Worlds', groups: 'Groups' }[type as 'users'] || 'Search')
          }
          onConnect={onLogin}
        />
      ) : (
        <>
          {error && (
            <div className="error-banner" role="alert">
              {error}
            </div>
          )}
          {busy ? (
            <div className="loading-state">
              <LoaderCircle className="spin" />
              {tr('Loading from VRChat…')}
            </div>
          ) : (
            <div className="search-result-grid">
              {visibleRows.map((r) => {
                if (section === 'Search' && type === 'avatars')
                  return (
                    <article className="search-result wide-image" key={r.id}>
                      <button
                        className="search-result-image"
                        tabIndex={-1}
                        aria-hidden="true"
                        onClick={() => onAvatar(r)}
                      >
                        <Image src={r.thumbnail} />
                      </button>
                      <div className="search-result-copy">
                        <h3>
                          <button className="search-result-name" onClick={() => onAvatar(r)}>
                            {r.name}
                          </button>
                        </h3>
                        <p>
                          {tr('by')} {r.author}
                          {r.pc && r.quest ? ' · PC + Quest' : r.quest ? ' · Quest' : ' · PC'}
                        </p>
                        <div className="result-actions">
                          <button className="standard-button" onClick={() => onAvatar(r)}>
                            {tr('View avatar')}
                            <ArrowUpRight size={13} />
                          </button>
                        </div>
                      </div>
                    </article>
                  );
                const id = r.groupId || r.id,
                  isGroup = section === 'Groups' || type === 'groups',
                  isWorld = section !== 'Groups' && type === 'worlds';
                const kind = isGroup ? 'group' : isWorld ? 'world' : 'user';
                return (
                  <article className={'search-result ' + (isWorld ? 'wide-image' : '')} key={id}>
                    <a
                      className="search-result-image"
                      tabIndex={-1}
                      aria-hidden="true"
                      {...detailLink(kind, id)}
                    >
                      {isGroup ? (
                        <Photo url={r.iconUrl} fallbackUrl={r.bannerUrl} name={r.name} />
                      ) : (
                        <Image
                          src={
                            isWorld
                              ? r.thumbnailImageUrl
                              : r.userIcon || r.profilePicOverride || r.currentAvatarThumbnailImageUrl
                          }
                        />
                      )}
                    </a>
                    <div className="search-result-copy">
                      <h3>
                        <a className="search-result-name" {...detailLink(kind, id)}>
                          {r.displayName || r.name}
                        </a>
                      </h3>
                      <p>
                        {isGroup
                          ? (r.shortCode || '') +
                            ' ' +
                            (r.discriminator || '') +
                            ' · ' +
                            (r.memberCount ?? '?') +
                            ' members'
                          : isWorld
                            ? 'by ' + r.authorName
                            : r.statusDescription || r.status || 'VRChat user'}
                      </p>
                      <div className="result-actions">
                        {isGroup ? (
                          <a className="standard-button" {...detailLink('group', id)}>
                            {tr('View group')}
                            <ArrowUpRight size={13} />
                          </a>
                        ) : isWorld ? (
                          <a className="standard-button" {...detailLink('world', id)}>
                            {tr('View world')}
                            <ArrowUpRight size={13} />
                          </a>
                        ) : (
                          <>
                            <a className="standard-button" {...detailLink('user', id)}>
                              {tr('Profile')}
                              <ArrowUpRight size={12} />
                            </a>
                            <button
                              className="standard-button"
                              onClick={() =>
                                setConfirm({
                                  action: 'friend',
                                  id,
                                  text: 'Send a friend request to ' + r.displayName + '?',
                                })
                              }
                            >
                              {tr('Add friend')}
                            </button>
                          </>
                        )}
                      </div>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
          {['Search', 'Users', 'Groups'].includes(section) &&
            !(section === 'Groups' && groupScope === 'mine') &&
            submitted !== null &&
            (rows.length > 0 || offset > 0) && (
              <div className="pagination">
                <span>
                  {tr('Results')} {rows.length ? offset + 1 : 0}–{offset + rows.length}
                </span>
                <div>
                  <button
                    disabled={!offset || busy}
                    onClick={() => search(null, Math.max(0, offset - (type === 'avatars' ? 36 : 50)))}
                  >
                    {tr('Previous')}
                  </button>
                  <button
                    disabled={rows.length < (type === 'avatars' ? 36 : 50) || busy}
                    onClick={() => search(null, offset + (type === 'avatars' ? 36 : 50))}
                  >
                    {tr('Next')}
                  </button>
                </div>
              </div>
            )}
          {(submitted !== null || (section === 'Groups' && groupScope === 'mine' && q.trim())) &&
            !visibleRows.length &&
            !busy &&
            !error && (
              <div className="empty-state">
                <Search size={28} />
                <h3>{tr('No results found')}</h3>
                <p>{tr('Try a different search.')}</p>
              </div>
            )}
          {section === 'Groups' && submitted === null && !q.trim() && !rows.length && !busy && !error && (
            <div className="empty-state">
              <Users size={28} />
              <h3>{tr(groupScope === 'mine' ? 'No groups to show' : 'Discover groups')}</h3>
              <p>
                {tr(
                  groupScope === 'mine'
                    ? mine
                      ? 'Use Groups in Discover to find a community to join.'
                      : 'Switch to All groups to find a community to join.'
                    : 'Enter a name to search all VRChat groups.',
                )}
              </p>
            </div>
          )}
        </>
      )}
      {confirm && (
        <div className="modal-backdrop">
          <div
            className="modal confirm-content"
            role="dialog"
            aria-modal="true"
            aria-label={tr('Confirm VRChat action')}
          >
            <h2>{tr('Confirm action')}</h2>
            <p>{confirm.text}</p>
            <button className="primary" disabled={busy} onClick={action}>
              {tr('Confirm')}
            </button>
            <button onClick={() => setConfirm(null)}>{tr('Cancel')}</button>
          </div>
        </div>
      )}
    </div>
  );
}
