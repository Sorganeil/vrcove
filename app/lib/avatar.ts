// A catalog avatar, or one normalized from a VRChat API record.
export type Avatar = {
  id: string;
  name: string;
  author: string;
  authorId?: string;
  description: string;
  image: string;
  thumbnail: string;
  pc: boolean;
  quest: boolean;
  rating: string;
  tags: string[];
  updated: string;
  created: string;
  favoriteId?: string;
};

// Converts a VRChat API avatar to the catalog shape. Impostor builds don't count as platform support.
export function normalize(a: any, favoriteId?: string): Avatar {
  const native = (a.unityPackages || []).filter((p: any) => p.variant !== 'impostor');
  return {
    id: a.id,
    name: typeof a.name === 'string' ? a.name : 'Untitled avatar',
    author: a.authorName || 'Unknown creator',
    description: a.description || '',
    image: a.imageUrl,
    thumbnail: a.thumbnailImageUrl,
    pc: native.some((p: any) => p.platform === 'standalonewindows'),
    quest: native.some((p: any) => p.platform === 'android'),
    rating: native.find((p: any) => p.platform === 'standalonewindows')?.performanceRating || 'Unknown',
    tags: a.tags || [],
    updated: a.updated_at,
    created: a.created_at,
    favoriteId,
  };
}
