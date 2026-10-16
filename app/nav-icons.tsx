import type { CSSProperties } from 'react';
type Props = { size?: number; className?: string; style?: CSSProperties };
function Glyph({ children, size = 20, ...props }: Props & { children: React.ReactNode }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" {...props}>
      {children}
    </svg>
  );
}
export const HomeIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M2 10.3 12 2l10 8.3-1.8 2.1-1.2-1V22h-5v-7h-4v7H5V11.4l-1.2 1z" />
  </Glyph>
);
export const AvatarIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M12 2a5 5 0 1 0 0 10 5 5 0 0 0 0-10ZM3 22v-3c0-4.2 4-6 9-6s9 1.8 9 6v3H3Z" />
  </Glyph>
);
export const UserIcon = (p: Props) => (
  <Glyph {...p}>
    <path
      fillRule="evenodd"
      d="M12 1a11 11 0 1 0 0 22 11 11 0 0 0 0-22Zm0 4a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7Zm0 15a8 8 0 0 1-6-2.7C6.8 15.3 9.2 14 12 14s5.2 1.3 6 3.3A8 8 0 0 1 12 20Z"
    />
  </Glyph>
);
export const FriendsIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm8 1a3.5 3.5 0 0 0-2 6.4 3.5 3.5 0 1 0 2-6.4ZM1 21v-3c0-3.3 3.5-5 8-5s8 1.7 8 5v3H1Zm18 0v-3c0-2.1-1-3.7-2.7-4.9 4-.2 6.7 1.5 6.7 4.9v3h-4Z" />
  </Glyph>
);
export const WorldIcon = (p: Props) => (
  <Glyph {...p}>
    <path
      fillRule="evenodd"
      d="M12 1a11 11 0 1 0 0 22 11 11 0 0 0 0-22ZM4 8h3c.3-1.7.7-3.1 1.3-4.3A9 9 0 0 0 4 8Zm-.9 2a9.7 9.7 0 0 0 0 4h3.5a24 24 0 0 1 0-4H3.1ZM4 16a9 9 0 0 0 4.3 4.3C7.7 19 7.3 17.7 7 16H4Zm8 4.5c1 0 2.2-1.9 2.8-4.5H9.2c.6 2.6 1.8 4.5 2.8 4.5ZM8.6 14h6.8a22 22 0 0 0 0-4H8.6a22 22 0 0 0 0 4Zm.6-6h5.6C14.2 5.4 13 3.5 12 3.5S9.8 5.4 9.2 8ZM17 8h3a9 9 0 0 0-4.3-4.3C16.3 5 16.7 6.3 17 8Zm.4 2a24 24 0 0 1 0 4h3.5a9.7 9.7 0 0 0 0-4h-3.5ZM17 16c-.3 1.7-.7 3.1-1.3 4.3A9 9 0 0 0 20 16h-3Z"
    />
  </Glyph>
);
export const FavoriteIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M12 22 2.7 12.7A6.3 6.3 0 0 1 12 4.3a6.3 6.3 0 0 1 9.3 8.4L12 22Z" />
  </Glyph>
);
export const GroupIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M12 2a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM4 7a3 3 0 1 0 0 6 3 3 0 0 0 0-6Zm16 0a3 3 0 1 0 0 6 3 3 0 0 0 0-6ZM5 22v-5c0-4 3-6 7-6s7 2 7 6v5H5Zm-5-2v-3c0-2.1 1.3-3.1 3.6-3.5-.4 1-.6 2.1-.6 3.5v3H0Zm21 0v-3c0-1.4-.2-2.5-.6-3.5C22.7 13.9 24 14.9 24 17v3h-3Z" />
  </Glyph>
);
export const SearchIcon = (p: Props) => (
  <Glyph {...p}>
    <path
      fillRule="evenodd"
      d="M10 1a9 9 0 1 0 5.6 16l6.1 6.1 2.2-2.2-6.1-6.1A9 9 0 0 0 10 1Zm0 3a6 6 0 1 1 0 12 6 6 0 0 1 0-12Z"
    />
  </Glyph>
);
export const BookmarkIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M5 2h14v21l-7-5-7 5V2Z" />
  </Glyph>
);
export const NotificationIcon = (p: Props) => (
  <Glyph {...p}>
    <path d="M12 1a2 2 0 0 1 2 2v.4c3.1.8 5 3.4 5 6.6v5l3 3v1H2v-1l3-3v-5c0-3.2 1.9-5.8 5-6.6V3a2 2 0 0 1 2-2Zm-3 20h6a3 3 0 0 1-6 0Z" />
  </Glyph>
);
export const SettingsIcon = (p: Props) => (
  <Glyph {...p}>
    <path
      fillRule="evenodd"
      d="m10 1-.7 3-2 .9L4.6 4 2 8.4l2.3 2a9.7 9.7 0 0 0 0 3.2L2 15.6 4.6 20l2.7-.9 2 .9.7 3h5l.7-3 2-.9 2.7.9 2.6-4.4-2.3-2a9.7 9.7 0 0 0 0-3.2l2.3-2L20.4 4l-2.7.9-2-.9L15 1h-5Zm2.5 7a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z"
    />
  </Glyph>
);
