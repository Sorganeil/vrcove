import { useEffect, useState } from 'react';
import { ArrowUpRight, RefreshCw, Search, UserRound, Users } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { useLoader } from '../lib/use-loader';
import { appLink } from '../lib/routes';
import { replaceSearchParams } from '../lib/search-params';
import { ErrorBanner, RequireVRChat } from '../components/section-page';
import SearchBar, { type SearchInput } from '../components/search-bar';
import Loading from '../components/loading';
import { detailLink } from '../components/detail-links';
import { Photo } from '../components/photo';
import AvatarResults from './avatar-search';
import WorldResults from './worlds';

type Tab = 'avatars' | 'worlds' | 'users' | 'groups';

const tabs = [
  ['avatars', 'Avatars', '/avatars'],
  ['worlds', 'Worlds', '/worlds'],
  ['users', 'Users', '/users'],
  ['groups', 'Groups', '/groups'],
] as const;

// Each section opens the search page on its tab, with its own heading.
const pages: Record<string, { tab: Tab; title: string; subtitle: string }> = {
  Search: { tab: 'avatars', title: 'Search VRChat', subtitle: 'Find avatars, people, worlds, and groups.' },
  Explore: {
    tab: 'avatars',
    title: 'Avatar search',
    subtitle: 'Public avatar catalog · search by name, creator, or avatar ID.',
  },
  Worlds: { tab: 'worlds', title: 'Worlds', subtitle: 'Popular public worlds, live from VRChat.' },
  Users: { tab: 'users', title: 'Users', subtitle: 'Find VRChat people by name.' },
  Groups: { tab: 'groups', title: 'Groups', subtitle: 'Discover communities across VRChat.' },
  MyGroups: { tab: 'groups', title: 'My groups', subtitle: 'Groups you belong to.' },
};

export const searchSections = Object.keys(pages);

const PAGE_SIZE = 50;

// The last avatar search, restored when returning to avatar search during the visit.
let lastAvatarQuery = '';

/**
 * One search for avatars, worlds, users and groups. Every tab has the same
 * search box, and the search text carries over when switching tabs. An empty
 * search shows the avatar catalog or popular worlds.
 */
export default function SearchPage({ section }: { section: string }) {
  useLocale();
  // VRChat results reload for a different account; the search text stays.
  const account = useApp().user?.id || 'guest';
  const page = pages[section] || pages.Search;
  const mine = section === 'MyGroups';
  const [input, setInput] = useState(() => {
    const linked = new URLSearchParams(window.location.search).get('q');
    return linked ?? (page.tab === 'avatars' ? lastAvatarQuery : '');
  });
  const [query, setQuery] = useState(() => input.trim());
  // Typed searches apply after a short pause; Enter applies them at once.
  useEffect(() => {
    const timer = setTimeout(() => setQuery(input.trim()), 350);
    return () => clearTimeout(timer);
  }, [input]);
  useEffect(() => {
    replaceSearchParams({ q: query || null });
    if (page.tab === 'avatars') lastAvatarQuery = query;
  }, [query, page.tab]);
  const search: SearchInput = { input, setInput, query, submit: () => setQuery(input.trim()) };
  return (
    <section className="catalog-section search-page">
      <div className="section-heading">
        <div>
          <h1>{tr(section === 'Explore' && query ? 'Avatar search results' : page.title)}</h1>
          <p>{tr(page.subtitle)}</p>
        </div>
      </div>
      {!mine && (
        <nav className="search-tabs" aria-label={tr('Search for')}>
          {tabs.map(([tab, label, path]) => (
            <a
              key={tab}
              aria-current={page.tab === tab ? 'page' : undefined}
              {...appLink(path + (query ? '?' + new URLSearchParams({ q: query }) : ''))}
            >
              {tr(label)}
            </a>
          ))}
        </nav>
      )}
      {page.tab === 'avatars' && <AvatarResults {...search} />}
      {page.tab === 'worlds' && <WorldResults key={account} {...search} />}
      {page.tab === 'users' && (
        <RequireVRChat section="Users">
          <UserList key={account} {...search} />
        </RequireVRChat>
      )}
      {page.tab === 'groups' && (
        <RequireVRChat section={mine ? 'My groups' : 'Groups'}>
          <GroupList key={account} {...search} mine={mine} />
        </RequireVRChat>
      )}
    </section>
  );
}

// VRChat answers some searches with a list and others with { results }.
const asList = (result: any): any[] => (Array.isArray(result) ? result : result?.results || []);

function searchVRChat(type: 'users' | 'groups', query: string, offset: number) {
  return api('/account/search?' + new URLSearchParams({ type, q: query, offset: String(offset) })).then(
    asList,
  );
}

/** The Users tab: VRChat people found by name. VRChat has no way to sort them. */
function UserList({ input, setInput, query, submit }: SearchInput) {
  useLocale();
  const { notify } = useApp();
  // The offset belongs to one search, so a new search starts at the first page.
  const [paging, setPaging] = useState({ query, offset: 0 });
  const offset = paging.query === query ? paging.offset : 0;
  const [confirm, setConfirm] = useState<{ id: string; name: string } | null>(null);
  const [sending, setSending] = useState(false);
  const { data, loading, error, retry } = useLoader(
    () => (query ? searchVRChat('users', query, offset) : Promise.resolve([])),
    [query, offset],
    [] as any[],
  );
  async function addFriend() {
    if (!confirm) return;
    setSending(true);
    try {
      await api('/account/action', { action: 'friend', id: confirm.id });
      notify(tr('VRChat confirmed the action.'));
      setConfirm(null);
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setSending(false);
    }
  }
  return (
    <>
      <SearchBar
        label="Search users"
        placeholder="Search users…"
        value={input}
        onChange={setInput}
        onSubmit={submit}
      />
      <ErrorBanner error={error} retry={retry} />
      {!query ? (
        <Empty icon={<Users size={28} />} title="Find people" text="Enter a name to search VRChat users." />
      ) : loading ? (
        <Loading text={tr('Loading from VRChat…')} />
      ) : data.length ? (
        <div className="search-result-grid">
          {data.map((r) => (
            <article className="search-result" key={r.id}>
              <a
                className="search-result-image"
                tabIndex={-1}
                aria-hidden="true"
                {...detailLink('user', r.id)}
              >
                <Thumbnail src={r.userIcon || r.profilePicOverride || r.currentAvatarThumbnailImageUrl} />
              </a>
              <div className="search-result-copy">
                <h3>
                  <a className="search-result-name" {...detailLink('user', r.id)}>
                    {r.displayName}
                  </a>
                </h3>
                <p>{r.statusDescription || r.status || 'VRChat user'}</p>
                <div className="result-actions">
                  <a className="standard-button" {...detailLink('user', r.id)}>
                    {tr('Profile')}
                    <ArrowUpRight size={12} />
                  </a>
                  <button
                    className="standard-button"
                    onClick={() => setConfirm({ id: r.id, name: r.displayName })}
                  >
                    {tr('Add friend')}
                  </button>
                </div>
              </div>
            </article>
          ))}
        </div>
      ) : (
        !error && <NoResults />
      )}
      {query && (
        <Pager
          offset={offset}
          count={data.length}
          loading={loading}
          onPage={(next) => setPaging({ query, offset: next })}
        />
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
            <p>{'Send a friend request to ' + confirm.name + '?'}</p>
            <button className="primary" disabled={sending} onClick={addFriend}>
              {tr('Confirm')}
            </button>
            <button onClick={() => setConfirm(null)}>{tr('Cancel')}</button>
          </div>
        </div>
      )}
    </>
  );
}

/**
 * The Groups tab: all VRChat groups found by name, or the groups you belong to,
 * filtered by the search. The My groups page shows only your groups.
 */
function GroupList({ input, setInput, query, submit, mine }: SearchInput & { mine: boolean }) {
  useLocale();
  const [scope, setScope] = useState<'mine' | 'all'>(mine ? 'mine' : 'all');
  const [paging, setPaging] = useState({ query, offset: 0 });
  const offset = paging.query === query ? paging.offset : 0;
  const memberships = useLoader(
    () => (scope === 'mine' ? api('/account/groups').then(asList) : Promise.resolve([])),
    [scope],
    [] as any[],
  );
  const found = useLoader(
    () => (scope === 'all' && query ? searchVRChat('groups', query, offset) : Promise.resolve([])),
    [scope, query, offset],
    [] as any[],
  );
  const current = scope === 'mine' ? memberships : found;
  const needle = query.toLocaleLowerCase();
  const rows =
    scope === 'mine'
      ? memberships.data.filter((g) =>
          [g.name, g.shortCode, g.discriminator]
            .filter(Boolean)
            .join(' ')
            .toLocaleLowerCase()
            .includes(needle),
        )
      : found.data;
  return (
    <>
      <SearchBar
        label="Search groups"
        placeholder={scope === 'mine' ? 'Search my groups…' : 'Search all groups…'}
        value={input}
        onChange={setInput}
        onSubmit={submit}
      />
      <div className="browse-bar">
        {!mine ? (
          <div className="chips" role="group" aria-label={tr('Group search scope')}>
            {(
              [
                ['mine', 'My groups'],
                ['all', 'All groups'],
              ] as const
            ).map(([value, label]) => (
              <button
                key={value}
                className={scope === value ? 'chosen' : ''}
                aria-pressed={scope === value}
                onClick={() => setScope(value)}
              >
                {tr(label)}
              </button>
            ))}
          </div>
        ) : (
          <span />
        )}
        {scope === 'mine' && (
          <button className="standard-button" onClick={memberships.retry} disabled={memberships.loading}>
            <RefreshCw size={13} />
            {tr('Refresh')}
          </button>
        )}
      </div>
      <ErrorBanner error={current.error} retry={current.retry} />
      {scope === 'all' && !query ? (
        <Empty
          icon={<Users size={28} />}
          title="Discover groups"
          text="Enter a name to search all VRChat groups."
        />
      ) : current.loading ? (
        <Loading text={tr('Loading from VRChat…')} />
      ) : rows.length ? (
        <div className="search-result-grid">
          {rows.map((r) => {
            const id = r.groupId || r.id;
            return (
              <article className="search-result" key={id}>
                <a
                  className="search-result-image"
                  tabIndex={-1}
                  aria-hidden="true"
                  {...detailLink('group', id)}
                >
                  <Photo url={r.iconUrl} fallbackUrl={r.bannerUrl} name={r.name} />
                </a>
                <div className="search-result-copy">
                  <h3>
                    <a className="search-result-name" {...detailLink('group', id)}>
                      {r.name}
                    </a>
                  </h3>
                  <p>
                    {(r.shortCode || '') +
                      ' ' +
                      (r.discriminator || '') +
                      ' · ' +
                      (r.memberCount ?? '?') +
                      ' members'}
                  </p>
                  <div className="result-actions">
                    <a className="standard-button" {...detailLink('group', id)}>
                      {tr('View group')}
                      <ArrowUpRight size={13} />
                    </a>
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      ) : current.error ? null : scope === 'mine' && !query ? (
        <Empty
          icon={<Users size={28} />}
          title="No groups to show"
          text={
            mine
              ? 'Use Groups in Discover to find a community to join.'
              : 'Switch to All groups to find a community to join.'
          }
        />
      ) : (
        <NoResults />
      )}
      {scope === 'all' && query && (
        <Pager
          offset={offset}
          count={found.data.length}
          loading={found.loading}
          onPage={(next) => setPaging({ query, offset: next })}
        />
      )}
    </>
  );
}

function Empty({ icon, title, text }: { icon: React.ReactNode; title: string; text: string }) {
  useLocale();
  return (
    <div className="empty-state">
      {icon}
      <h3>{tr(title)}</h3>
      <p>{tr(text)}</p>
    </div>
  );
}

const NoResults = () => (
  <Empty icon={<Search size={28} />} title="No results found" text="Try a different search." />
);

/** Previous and Next for VRChat searches, which return up to 50 results at a time. */
function Pager({
  offset,
  count,
  loading,
  onPage,
}: {
  offset: number;
  count: number;
  loading: boolean;
  onPage: (offset: number) => void;
}) {
  useLocale();
  if (!count && !offset) return null;
  return (
    <div className="pagination">
      <span>
        {tr('Results')} {count ? offset + 1 : 0}–{offset + count}
      </span>
      <div>
        <button disabled={!offset || loading} onClick={() => onPage(Math.max(0, offset - PAGE_SIZE))}>
          {tr('Previous')}
        </button>
        <button disabled={count < PAGE_SIZE || loading} onClick={() => onPage(offset + PAGE_SIZE)}>
          {tr('Next')}
        </button>
      </div>
    </div>
  );
}

/** A proxied VRChat picture, or a placeholder when it can't load. */
function Thumbnail({ src }: { src?: string }) {
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
