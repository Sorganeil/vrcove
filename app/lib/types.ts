// Shapes of the data VRCove's own API returns. VRChat records pass through mostly unfiltered,
// so their types list the fields the UI reads and allow anything else.

/** A VRChat user as returned by /api/account/me, user lookups and friend lists. */
export type VRChatUser = {
  id: string;
  displayName: string;
  bio?: string;
  bioLinks?: string[];
  status?: string;
  statusDescription?: string;
  state?: string;
  location?: string;
  tags?: string[];
  userIcon?: string;
  profilePicOverride?: string;
  currentAvatarImageUrl?: string;
  currentAvatarThumbnailImageUrl?: string;
  [field: string]: unknown;
};

/** One device's VRChat session for a linked account. */
export type VRChatSession = {
  id: number;
  device: 'phone' | 'tablet' | 'computer';
  browser: string;
  started: string;
  expires: number;
  current: boolean;
};

/** A VRChat account linked to the signed-in VRCove account. */
export type LinkedAccount = {
  id: number;
  vrchatId: string;
  displayName: string;
  username?: string;
  linked: string;
  active?: boolean;
  sessionExpires?: number;
  sessions?: VRChatSession[];
};

/** The signed-in VRCove account (/api/cove/me). */
export type CoveAccount = {
  username: string;
  created: string;
  bio: string;
  status: string;
  public: boolean;
  picture: string;
  /** Moderation role; admins and owners see the hidden admin page. */
  role?: 'user' | 'admin' | 'owner';
  recoveryCodesRemaining: number;
  showLinkReminder?: boolean;
  links: LinkedAccount[];
};

/** Catalog size and deployment configuration (/api/stats). */
export type Stats = {
  indexed: number;
  sourcePublic?: number;
  privateExcluded?: number;
  loginConfigured?: boolean;
  accountsConfigured?: boolean;
  turnstileSiteKey?: string;
};

/** A world in lists and favorites. */
export type WorldSummary = {
  id: string;
  name: string;
  authorName?: string;
  thumbnailImageUrl?: string;
  imageUrl?: string;
  occupants?: number;
  favoriteId?: string;
  [field: string]: unknown;
};

/** A VRChat notification. */
export type VRChatNotification = {
  id: string;
  type: string;
  senderUsername?: string;
  message?: string;
  created_at: string;
  seen?: boolean;
};

/** A VRChat avatar favorite group. */
export type FavoriteGroup = { name: string; displayName?: string };

/** A VRCove review, on an item's page (with its author) or on a profile (with its target). */
export type Review = {
  id?: number;
  kind: 'user' | 'world' | 'avatar' | 'group';
  target?: string;
  name?: string;
  author?: string;
  profile?: boolean;
  mine?: boolean;
  rating: number;
  body: string;
  date: string;
  edited?: boolean;
};

/** An avatar or world saved in a VRCove collection. */
export type CollectionItem = { id: string; name: string; image: string; added: string };

/** An avatar record as VRChat returns it, before conversion to the catalog shape. */
export type RawAvatar = {
  id: string;
  name?: unknown;
  authorName?: string;
  description?: string;
  imageUrl?: string;
  thumbnailImageUrl?: string;
  unityPackages?: { platform?: string; variant?: string; performanceRating?: string }[];
  tags?: string[];
  updated_at?: string;
  created_at?: string;
};
