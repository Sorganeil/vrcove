import { useState } from 'react';
import { ArrowUpRight, Search, Users, X } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useLoader } from '../lib/use-loader';
import { Picture } from '../components/avatar-media';
import Loading from '../components/loading';
import Pager from '../components/pager';
import UserStatus, { notInGame } from '../components/user-status';
import { detailLink } from '../components/detail-links';
import { ErrorBanner, RequireVRChat, SectionPage } from '../components/section-page';
import type { VRChatUser } from '../lib/types';

/** Online and offline VRChat friends. */
export default function Friends() {
  return (
    <SectionPage title="Friends" subtitle="Online and offline friends from your VRChat account.">
      <RequireVRChat section="Friends">
        <FriendList />
      </RequireVRChat>
    </SectionPage>
  );
}

type FriendFavorites = { groups: { name: string; label: string }[]; groupOf: Record<string, string> };

// VRChat's friend favorite groups (group_0, group_1, ...) and who is in each.
async function loadFriendFavorites(isAlive: () => boolean): Promise<FriendFavorites> {
  const groups: { name: string; displayName?: string }[] = await api('/account/favorite-groups?type=friend');
  const groupOf: Record<string, string> = {};
  for (let offset = 0; isAlive(); offset += 100) {
    const page: { favoriteId?: string; tags?: string[] }[] = await api(
      '/account/favorites?type=friend&offset=' + offset,
    );
    for (const f of page) if (f.favoriteId && f.tags?.[0]) groupOf[f.favoriteId] = f.tags[0];
    if (page.length < 100) break;
  }
  return {
    groups: groups
      .map((g) => ({ name: g.name, label: g.displayName || g.name }))
      .sort((a, b) => a.name.localeCompare(b.name)),
    groupOf,
  };
}

const sortKey = 'vrcove-friend-sort';

function FriendList() {
  useLocale();
  const [sort, setSort] = useState(() => {
    try {
      const saved = localStorage.getItem(sortKey);
      return saved === 'favorites' || saved === 'name' ? saved : 'status';
    } catch {
      return 'status';
    }
  });
  const [search, setSearch] = useState('');
  const [offline, setOffline] = useState(false);
  const [offset, setOffset] = useState(0);
  const searching = Boolean(search.trim());
  // Searching looks through every friend, so all pages are loaded.
  const { data, loading, error, retry } = useLoader(
    async (isAlive) => {
      let rows: VRChatUser[] = await api(
        '/account/friends?offline=' + offline + '&offset=' + (searching ? 0 : offset),
      );
      if (searching) {
        const all = [...rows];
        while (rows.length === 100 && isAlive()) {
          rows = await api('/account/friends?offline=' + offline + '&offset=' + all.length);
          all.push(...rows);
        }
        return all;
      }
      return rows;
    },
    [offline, offset, searching],
    [] as VRChatUser[],
  );
  const favorites = useLoader(
    (isAlive) => (sort === 'favorites' ? loadFriendFavorites(isAlive) : Promise.resolve(null)),
    [sort],
    null as FriendFavorites | null,
  );
  const needle = search.trim().toLocaleLowerCase();
  const friends = data.filter(
    (f) =>
      !searching ||
      [f.displayName, f.id, f.statusDescription].some((value) =>
        String(value || '')
          .toLocaleLowerCase()
          .includes(needle),
      ),
  );
  return (
    <>
      <ErrorBanner error={error} retry={retry} />
      <div className="search-row">
        <div className="search-input">
          <Search size={19} />
          <input
            type="search"
            aria-label={tr('Search friends')}
            placeholder={tr('Search friends…')}
            value={search}
            maxLength={100}
            onChange={(e) => {
              setSearch(e.target.value);
              setOffset(0);
            }}
          />
          {search && (
            <button
              aria-label={tr('Clear search')}
              onClick={() => {
                setSearch('');
                setOffset(0);
              }}
            >
              <X size={16} />
            </button>
          )}
        </div>
      </div>
      <div className="browse-bar">
        <div className="chips">
          <button
            className={!offline ? 'chosen' : ''}
            onClick={() => {
              setOffline(false);
              setOffset(0);
            }}
          >
            {tr('Online')}
          </button>
          <button
            className={offline ? 'chosen' : ''}
            onClick={() => {
              setOffline(true);
              setOffset(0);
            }}
          >
            {tr('Offline')}
          </button>
        </div>
        <label className="sort-label">
          {tr('Sort by')}
          <select
            aria-label={tr('Sort friends')}
            value={sort}
            onChange={(e) => {
              setSort(e.target.value);
              try {
                localStorage.setItem(sortKey, e.target.value);
              } catch {
                // Sorting still works for this visit.
              }
            }}
          >
            <option value="status">{tr('In game first')}</option>
            <option value="favorites">{tr('Favorite groups')}</option>
            <option value="name">{tr('Name A–Z')}</option>
          </select>
        </label>
      </div>
      <p className="muted friend-count">{tr('{count} friends on this page', { count: friends.length })}</p>
      {sort === 'favorites' && favorites.error && (
        <p className="fineprint">{tr('Couldn’t load your favorite groups.')}</p>
      )}
      {loading || (sort === 'favorites' && favorites.loading) ? (
        <Loading text={tr('Finding your people…')} />
      ) : (
        sections(friends, offline, sort === 'favorites' ? favorites.data : null, sort === 'name').map(
          (section) => (
            <section className="friend-section" key={section.key}>
              {section.title && (
                <h2>
                  {section.title}
                  <small>{section.friends.length}</small>
                </h2>
              )}
              <div className="friend-grid">
                {section.friends.map((f) => (
                  <a className="friend-card" key={f.id} {...detailLink('user', f.id)}>
                    <div className="friend-photo">
                      <Picture
                        src={f.userIcon || f.profilePicOverride || f.currentAvatarThumbnailImageUrl}
                        alt=""
                      />
                    </div>
                    <div>
                      <h3>{f.displayName}</h3>
                      <UserStatus user={f} offline={offline} />
                      <small>
                        {notInGame(f, offline)
                          ? tr('On the website or app')
                          : f.location === 'private'
                            ? 'In a private instance'
                            : f.location === 'offline'
                              ? tr('Offline')
                              : f.location === 'traveling'
                                ? 'Traveling'
                                : tr('View profile')}
                      </small>
                    </div>
                    <ArrowUpRight size={17} />
                  </a>
                ))}
              </div>
            </section>
          ),
        )
      )}
      {!loading && !friends.length && !error && (
        <div className="empty-state">
          <Users />
          <h3>{tr(searching ? 'No friends found' : 'No friends to show here.')}</h3>
          <p>
            {tr(
              searching
                ? 'Try a different search or the other tab.'
                : 'Try the other tab, or add friends in VRChat.',
            )}
          </p>
        </div>
      )}
      {!searching && <Pager offset={offset} setOffset={setOffset} count={data.length} busy={loading} />}
    </>
  );
}

type Section = { key: string; title?: string; friends: VRChatUser[] };

/** Splits a page of friends into favorite groups, into in game and not in game, or lists them by name. */
function sections(
  friends: VRChatUser[],
  offline: boolean,
  favorites: FriendFavorites | null,
  byName = false,
): Section[] {
  const inGameFirst = (list: VRChatUser[]) =>
    [...list].sort((a, b) => Number(notInGame(a, offline)) - Number(notInGame(b, offline)));
  let out: Section[];
  if (byName) {
    out = [
      {
        key: 'all',
        friends: [...friends].sort((a, b) =>
          (a.displayName || '').localeCompare(b.displayName || '', undefined, { sensitivity: 'base' }),
        ),
      },
    ];
  } else if (favorites) {
    const known = new Set(favorites.groups.map((g) => g.name));
    out = [
      ...favorites.groups.map((g) => ({
        key: g.name,
        title: g.label,
        friends: inGameFirst(friends.filter((f) => favorites.groupOf[f.id] === g.name)),
      })),
      {
        key: 'other',
        title: tr('Not in a favorite group'),
        friends: inGameFirst(friends.filter((f) => !known.has(favorites.groupOf[f.id]))),
      },
    ];
  } else if (!offline) {
    out = [
      { key: 'game', title: tr('In game'), friends: friends.filter((f) => !notInGame(f)) },
      { key: 'web', title: tr('Not in game'), friends: friends.filter((f) => notInGame(f)) },
    ];
  } else {
    out = [{ key: 'all', friends }];
  }
  return out.filter((section) => section.friends.length);
}
