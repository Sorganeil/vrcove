import { useState } from 'react';
import { ArrowUpRight, Search, Users, X } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useLoader } from '../lib/use-loader';
import { Picture } from '../components/avatar-media';
import Loading from '../components/loading';
import Pager from '../components/pager';
import UserStatus from '../components/user-status';
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

function FriendList() {
  useLocale();
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
        <span className="muted">{tr('{count} friends on this page', { count: friends.length })}</span>
      </div>
      {loading ? (
        <Loading text={tr('Finding your people…')} />
      ) : (
        <div className="friend-grid">
          {friends.map((f) => (
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
                  {f.location === 'private'
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
