import { useEffect, useRef, useState } from 'react';
import { entityPath } from './routes';

export type DetailKind = 'world' | 'user' | 'group';
export type Route = {
  section: string;
  /** VRCove username or collection ID, for the CoveProfile and Collection sections. */
  coveTarget: string;
  avatarId: string;
  detail: { kind: DetailKind; id: string } | null;
  /** Increases on every navigation, so pages can start fresh when reopened. */
  version: number;
};

const sectionNames: Record<string, string> = {
  home: 'Home',
  avatars: 'Explore',
  friends: 'Friends',
  worlds: 'Worlds',
  users: 'Users',
  favorites: 'Favorites',
  search: 'Search',
  groups: 'Groups',
  'my-groups': 'MyGroups',
  bookmarks: 'Bookmarks',
  profile: 'Profile',
  notifications: 'Notifications',
  settings: 'Settings',
  donations: 'Donations',
  about: 'About',
  companion: 'Companion',
  admin: 'Admin',
  uploads: 'Uploads',
  account: 'Account',
  collections: 'Collections',
};

// Sections use clean paths (/avatars, /about) that the server renders and search engines can index.
export function sectionPath(section: string) {
  const slug =
    section === 'Explore' ? 'avatars' : section === 'MyGroups' ? 'my-groups' : section.toLowerCase();
  return slug === 'home' ? '/' : '/' + slug;
}

/**
 * Reads the current URL. Legacy #routes and /veil/ links are rewritten to their shareable paths.
 * `scroll` is true for detail pages, which always open at the top.
 */
function parseLocation(): Omit<Route, 'version'> & { scroll: boolean } {
  const route = window.location.hash ? window.location.hash.slice(1) : window.location.pathname.slice(1);
  const slug = route.toLowerCase();
  const empty = { coveTarget: '', avatarId: '', detail: null };
  const legacy = /^veil\/([A-Za-z0-9_.-]{3,32})$/.exec(route);
  if (legacy) window.history.replaceState(null, '', '/cove/' + legacy[1]);
  const cove = /^(?:cove|veil)\/([A-Za-z0-9_.-]{3,32})$/.exec(route),
    collection = /^collection\/([A-Za-z0-9_-]{16})$/.exec(route);
  if (cove || collection) {
    window.history.replaceState(null, '', cove ? '/cove/' + cove[1] : '/collection/' + collection![1]);
    return {
      ...empty,
      section: cove ? 'CoveProfile' : 'Collection',
      coveTarget: cove ? cove[1] : collection![1],
      scroll: true,
    };
  }
  const avatar = /^avatar\/(avtr_[a-f0-9-]{36})$/.exec(route);
  if (avatar) {
    window.history.replaceState(null, '', entityPath('avatar', avatar[1]));
    return { ...empty, section: 'Avatar', avatarId: avatar[1], scroll: true };
  }
  const page = /^(world|user|group)\/((?:wrld|usr|grp)_[a-f0-9-]{36}|[A-Za-z0-9]{10})$/.exec(route);
  if (
    page &&
    (page[2].startsWith({ world: 'wrld_', user: 'usr_', group: 'grp_' }[page[1]]!) ||
      (page[1] === 'user' && page[2].length === 10))
  ) {
    const kind = page[1] as DetailKind;
    window.history.replaceState(null, '', entityPath(kind, page[2]));
    return {
      ...empty,
      section: kind[0].toUpperCase() + kind.slice(1),
      detail: { kind, id: page[2] },
      scroll: true,
    };
  }
  const known = sectionNames[slug];
  if (window.location.hash && known) window.history.replaceState(null, '', sectionPath(known));
  return { ...empty, section: known || 'Home', scroll: false };
}

/** The current route, kept in sync with history. `onChange` runs after back/forward and in-app links. */
export function useRoute(onChange: (scroll: boolean) => void) {
  const [route, setRoute] = useState<Route>(() => {
    const { scroll: _scroll, ...parsed } = parseLocation();
    void _scroll;
    return { ...parsed, version: 0 };
  });
  const changed = useRef(onChange);
  useEffect(() => {
    changed.current = onChange;
  });
  useEffect(() => {
    const restore = () => {
      const { scroll, ...parsed } = parseLocation();
      setRoute((r) => ({ ...parsed, version: r.version + 1 }));
      changed.current(scroll);
    };
    window.addEventListener('popstate', restore);
    window.addEventListener('hashchange', restore);
    return () => {
      window.removeEventListener('popstate', restore);
      window.removeEventListener('hashchange', restore);
    };
  }, []);
  function navigate(section: string) {
    const path = sectionPath(section);
    if (window.location.pathname !== path || window.location.hash || window.location.search)
      window.history.pushState(null, '', path);
    setRoute((r) => ({ section, coveTarget: '', avatarId: '', detail: null, version: r.version + 1 }));
  }
  return { route, navigate };
}
