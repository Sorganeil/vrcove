import { createContext, useContext, type MouseEvent } from 'react';
import type { Avatar } from './avatar';
import type { CoveAccount, Stats, VRChatUser } from './types';

export type AvatarBrowse = { author?: { name: string; id?: string }; tag?: string };

/** App-wide state and actions shared by pages, provided once by App. */
export type AppContextValue = {
  /** The connected VRChat user, or null. */
  user: VRChatUser | null;
  setUser: (user: VRChatUser | null) => void;
  /** The signed-in VRCove account, or null. */
  coveAccount: CoveAccount | null;
  setCoveAccount: (account: CoveAccount | null) => void;
  /** Applies a VRCove sign-in, sign-out or account change everywhere. */
  accountChanged: (account: CoveAccount | null) => void;
  stats: Stats | null;
  section: string;
  navigate: (section: string) => void;
  sectionLink: (section: string) => { href: string; onClick: (e: MouseEvent) => void };
  notify: (message: string) => void;
  /** Opens the VRChat connection dialog, preselecting a linked account when given. */
  requestConnection: (expectedUserId?: string, username?: string) => void;
  /** Avatars bookmarked in this browser. */
  saved: Avatar[];
  setSaved: (avatars: Avatar[]) => void;
  bookmark: (avatar: Avatar) => void;
  /** Avatar quick view. */
  openAvatar: (avatar: Avatar) => void;
  closeAvatar: () => void;
  openPrivacy: () => void;
  /** Opens avatar search filtered to a creator or tag. */
  browseAvatars: (filter: AvatarBrowse) => void;
};

export const AppContext = createContext<AppContextValue | null>(null);

export function useApp() {
  const value = useContext(AppContext);
  if (!value) throw new Error('useApp must be used inside App');
  return value;
}
