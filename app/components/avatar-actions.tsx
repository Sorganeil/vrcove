import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react';
import { Heart, RefreshCw, ShieldCheck, LoaderCircle, Check } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import type { Avatar } from '../lib/avatar';
import Modal from './modal';
import type { FavoriteGroup } from '../lib/types';

type AvatarActions = {
  /** Name of the action in progress, or ''. */
  busy: string;
  /** The VRChat favorite ID of an avatar, if it is favorited. */
  favoriteOf: (avatar: Avatar) => string | undefined;
  /** Loads which avatars are favorited, once per session. */
  loadFavorites: () => void;
  wear: (avatar: Avatar) => void;
  favorite: (avatar: Avatar) => void;
  fallback: (avatar: Avatar) => void;
  unfavorite: (avatar: Avatar) => void;
  /** Avatars unfavorited since lists were loaded, so lists can drop them without refetching. */
  unfavorited: ReadonlySet<string>;
};

const AvatarActionsContext = createContext<AvatarActions | null>(null);

export function useAvatarActions() {
  const value = useContext(AvatarActionsContext);
  if (!value) throw new Error('useAvatarActions must be used inside AvatarActionsProvider');
  return value;
}

/** Wearing, favoriting and fallback actions for avatars, with their confirmation dialogs. */
export function AvatarActionsProvider({ children }: { children: ReactNode }) {
  const { user, setUser, notify, requestConnection, closeAvatar } = useApp();
  const [busy, setBusy] = useState('');
  // Favorite IDs by avatar, tagged with the VRChat user they were loaded for.
  const [favorites, setFavorites] = useState<{ owner: string; ids: Record<string, string> } | null>(null);
  const favoriteIds = favorites && favorites.owner === user?.id ? favorites.ids : null;
  const [unfavorited, setUnfavorited] = useState<ReadonlySet<string>>(new Set());
  const [favoriteDialog, setFavoriteDialog] = useState<Avatar | null>(null);
  const [switchDialog, setSwitchDialog] = useState<Avatar | null>(null);
  const [fallbackDialog, setFallbackDialog] = useState<Avatar | null>(null);
  const [groups, setGroups] = useState<FavoriteGroup[]>([]);
  const [group, setGroup] = useState('');
  const loading = useRef(false);

  // A different VRChat account starts with a fresh favorite lookup and no open dialogs.
  const [userId, setUserId] = useState(user?.id);
  if (userId !== user?.id) {
    setUserId(user?.id);
    setUnfavorited(new Set());
    setFavoriteDialog(null);
    setSwitchDialog(null);
    setFallbackDialog(null);
  }

  // Avatars opened outside the Favorites page learn their favorite status from one lookup per session.
  const loadFavorites = useCallback(() => {
    if (!user || favoriteIds || loading.current) return;
    loading.current = true;
    const owner = user.id;
    (async () => {
      const ids: Record<string, string> = {};
      for (let offset = 0; offset < 1000; offset += 100) {
        const rows = await api('/account/favorites?type=avatar&offset=' + offset);
        for (const f of rows) if (f?.favoriteId && f?.id) ids[f.favoriteId] = f.id;
        if (rows.length < 100) break;
      }
      setFavorites({ owner, ids });
    })()
      .catch(() => {})
      .finally(() => {
        loading.current = false;
      });
  }, [user, favoriteIds]);

  const favoriteOf = (a: Avatar) => a.favoriteId || favoriteIds?.[a.id];

  async function favorite(a: Avatar) {
    if (!user) {
      requestConnection();
      return;
    }
    if (favoriteOf(a)) {
      notify(tr('This avatar is already in your VRChat favorites.'));
      return;
    }
    setBusy('groups');
    try {
      const g = await api('/account/favorite-groups');
      setGroups(g);
      setGroup(g[0]?.name || '');
      setFavoriteDialog(a);
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  async function perform(action: 'select' | 'favorites' | 'unfavorite', a: Avatar) {
    setBusy(action);
    try {
      const result = await api(
        '/account/' + action,
        action === 'select'
          ? { avatarId: a.id }
          : action === 'favorites'
            ? { avatarId: a.id, group }
            : { favoriteId: favoriteOf(a) },
      );
      if (action === 'favorites')
        setFavorites((f) => (result?.id && f ? { ...f, ids: { ...f.ids, [a.id]: result.id } } : null));
      if (action === 'unfavorite') {
        setFavorites((f) => {
          if (!f) return f;
          const ids = { ...f.ids };
          delete ids[a.id];
          return { ...f, ids };
        });
        setUnfavorited((s) => new Set(s).add(a.id));
        closeAvatar();
      }
      notify(
        action === 'select'
          ? 'Avatar changed in VRChat'
          : action === 'favorites'
            ? 'Added to your VRChat favorites'
            : 'Removed from VRChat favorites',
      );
      setFavoriteDialog(null);
      setSwitchDialog(null);
      if (action === 'select')
        api('/account/me')
          .then(setUser)
          .catch(() => {});
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  async function setFallback(a: Avatar) {
    setBusy('fallback');
    try {
      await api('/account/action', { action: 'fallback', id: a.id });
      setFallbackDialog(null);
      notify(tr('Fallback avatar updated in VRChat'));
      api('/account/me')
        .then(setUser)
        .catch(() => {});
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  const value: AvatarActions = {
    busy,
    favoriteOf,
    loadFavorites,
    wear: (a) => (user ? setSwitchDialog(a) : requestConnection()),
    favorite: (a) => void favorite(a),
    fallback: (a) => (user ? setFallbackDialog(a) : requestConnection()),
    unfavorite: (a) => void perform('unfavorite', a),
    unfavorited,
  };

  return (
    <AvatarActionsContext.Provider value={value}>
      {children}
      {favoriteDialog && (
        <Modal title={tr('Favorite avatar')} close={() => setFavoriteDialog(null)}>
          <div className="confirm-content">
            <Heart size={28} />
            <h2>{tr('Favorite avatar')}</h2>
            <p>
              {tr('Add')} <strong>{favoriteDialog.name}</strong> {tr('to a VRChat favorite group.')}
            </p>
            <label>
              {tr('Favorite group')}
              <select value={group} onChange={(e) => setGroup(e.target.value)}>
                {groups.map((g) => (
                  <option key={g.name} value={g.name}>
                    {g.displayName || g.name}
                  </option>
                ))}
              </select>
            </label>
            {!groups.length && (
              <p>{tr('No avatar favorite groups were returned. Create one in VRChat, then try again.')}</p>
            )}
            <button
              className="primary"
              disabled={!group || !!busy}
              onClick={() => perform('favorites', favoriteDialog)}
            >
              {busy ? <LoaderCircle className="spin" size={16} /> : <Heart size={16} />}
              {tr('Add favorite')}
            </button>
          </div>
        </Modal>
      )}
      {switchDialog && (
        <Modal title={tr('Change avatar')} close={() => setSwitchDialog(null)}>
          <div className="confirm-content">
            <RefreshCw size={28} />
            <h2>{tr('Change avatar')}</h2>
            <p>
              {tr('Switch your VRChat account to')} <strong>{switchDialog.name}</strong>
              {tr('? This changes your active avatar in VRChat.')}
            </p>
            <button className="primary" disabled={!!busy} onClick={() => perform('select', switchDialog)}>
              {busy ? <LoaderCircle className="spin" size={16} /> : <Check size={16} />}
              {tr('Switch avatar')}
            </button>
            <button onClick={() => setSwitchDialog(null)}>{tr('Keep my current avatar')}</button>
          </div>
        </Modal>
      )}
      {fallbackDialog && (
        <Modal title={tr('Set fallback avatar')} close={() => setFallbackDialog(null)}>
          <div className="confirm-content">
            <ShieldCheck size={28} />
            <h2>{tr('Set fallback avatar?')}</h2>
            <p>
              {tr('Use')} <strong>{fallbackDialog.name}</strong>{' '}
              {tr(
                'when other players cannot display your avatar? VRChat checks whether this avatar is eligible.',
              )}
            </p>
            <button className="primary" disabled={!!busy} onClick={() => setFallback(fallbackDialog)}>
              {busy ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />}
              {tr('Set fallback avatar')}
            </button>
            <button disabled={!!busy} onClick={() => setFallbackDialog(null)}>
              {tr('Cancel')}
            </button>
          </div>
        </Modal>
      )}
    </AvatarActionsContext.Provider>
  );
}
