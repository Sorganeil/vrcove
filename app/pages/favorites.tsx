import { useState } from 'react';
import { Search } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useLoader } from '../lib/use-loader';
import { normalize, type Avatar } from '../lib/avatar';
import AvatarGrid from '../components/avatar-grid';
import Loading from '../components/loading';
import Pager from '../components/pager';
import WorldGrid from '../components/world-grid';
import { useAvatarActions } from '../components/avatar-actions';
import { ErrorBanner, RequireVRChat, SectionPage } from '../components/section-page';

type FavoriteType = 'avatar' | 'world';

/** Avatar and world favorites from the VRChat account. */
export default function Favorites() {
  return (
    <SectionPage title="Favorites" subtitle="Your avatar and world favorites, synced from VRChat.">
      <RequireVRChat section="Favorites">
        <FavoriteList />
      </RequireVRChat>
    </SectionPage>
  );
}

/** Looks up one favorite: the catalog first for avatars, then VRChat, or a placeholder if unavailable. */
async function resolveFavorite(type: FavoriteType, f: any) {
  try {
    if (type === 'world')
      return { ...(await api('/account/world?id=' + encodeURIComponent(f.favoriteId))), favoriteId: f.id };
    const found = await api('/catalog?q=' + encodeURIComponent(f.favoriteId));
    return found.avatars[0]
      ? { ...found.avatars[0], favoriteId: f.id }
      : normalize(await api('/account/avatar?id=' + f.favoriteId), f.id);
  } catch {
    if (type === 'world')
      return {
        id: f.favoriteId,
        name: 'Unavailable world',
        authorName: 'This favorite may be private, deleted, or unavailable.',
        favoriteId: f.id,
      };
    return {
      id: f.favoriteId,
      name: 'Unavailable avatar',
      author: 'Not in the public catalog',
      description: 'This favorite may be private, deleted, or unavailable.',
      image: '',
      thumbnail: '',
      pc: false,
      quest: false,
      rating: 'Unknown',
      tags: [],
      created: '',
      updated: '',
      favoriteId: f.id,
    };
  }
}

function FavoriteList() {
  useLocale();
  const { unfavorited } = useAvatarActions();
  const [type, setType] = useState<FavoriteType>('avatar');
  const [offset, setOffset] = useState(0);
  const { data, loading, error, retry } = useLoader(
    async (isAlive) => {
      const rows = await api('/account/favorites?type=' + type + '&offset=' + offset);
      const list: any[] = [];
      // Six lookups at a time keeps VRChat from rate limiting.
      for (let i = 0; i < rows.length && isAlive(); i += 6)
        list.push(...(await Promise.all(rows.slice(i, i + 6).map((f: any) => resolveFavorite(type, f)))));
      return list;
    },
    [type, offset],
    [] as any[],
  );
  // Never draw leftover world rows as avatar cards while switching.
  const avatars = (data as Avatar[]).filter(
    (a) => typeof a?.id === 'string' && a.id.startsWith('avtr_') && !unfavorited.has(a.id),
  );
  return (
    <>
      <ErrorBanner error={error} retry={retry} />
      <div className="browse-bar">
        <div className="chips" aria-label={tr('Favorite type')}>
          {(['avatar', 'world'] as const).map((t) => (
            <button
              key={t}
              className={type === t ? 'chosen' : ''}
              aria-pressed={type === t}
              onClick={() => {
                setType(t);
                setOffset(0);
              }}
            >
              {tr(t === 'avatar' ? 'Avatars' : 'Worlds')}
            </button>
          ))}
        </div>
      </div>
      {type === 'avatar' ? (
        <>
          <div className="results-line">
            <span>{tr('{count} favorites on this page', { count: avatars.length })}</span>
            <span>{tr('YOUR VRCHAT ACCOUNT')}</span>
          </div>
          {loading || avatars.length ? (
            <AvatarGrid avatars={avatars} loading={loading} />
          ) : (
            <div className="empty-state">
              <Search size={30} />
              <h3>{tr('No avatars found')}</h3>
              <p>{tr('Try a different search or loosen your filters.')}</p>
            </div>
          )}
        </>
      ) : (
        <>
          {loading ? <Loading text={tr('Exploring worlds…')} /> : <WorldGrid worlds={data} />}
          {!loading && !data.length && !error && (
            <div className="empty-state">
              <Search size={30} />
              <h3>{tr('No favorite worlds found')}</h3>
              <p>{tr('Save worlds in VRChat to see them here.')}</p>
            </div>
          )}
        </>
      )}
      <Pager offset={offset} setOffset={setOffset} count={data.length} busy={loading} />
    </>
  );
}
