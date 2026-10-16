import { Library, Upload, KeyRound } from 'lucide-react';
import {
  HomeIcon,
  AvatarIcon,
  UserIcon,
  FriendsIcon,
  WorldIcon,
  FavoriteIcon,
  GroupIcon,
  SearchIcon,
  BookmarkIcon,
  NotificationIcon,
  SettingsIcon,
} from '../components/nav-icons';

// Sidebar sections. `name` is the section id, `label` the untranslated menu text.
export const navigationGroups = [
  {
    label: 'Discover',
    items: [
      { name: 'Explore', label: 'Avatars', icon: AvatarIcon },
      { name: 'Worlds', label: 'Worlds', icon: WorldIcon },
      { name: 'Users', label: 'Users', icon: UserIcon },
      { name: 'Groups', label: 'Groups', icon: GroupIcon },
    ],
  },
  {
    label: 'Your library',
    items: [
      { name: 'Bookmarks', label: 'Bookmarks', icon: BookmarkIcon },
      { name: 'Collections', label: 'Collections', icon: Library },
      { name: 'Favorites', label: 'Favorites', icon: FavoriteIcon },
      { name: 'Uploads', label: 'My uploads', icon: Upload },
    ],
  },
  {
    label: 'VRChat',
    items: [
      { name: 'Friends', label: 'Friends', icon: FriendsIcon },
      { name: 'Notifications', label: 'Notifications', icon: NotificationIcon },
      { name: 'MyGroups', label: 'My groups', icon: GroupIcon },
      { name: 'Profile', label: 'My profile', icon: UserIcon },
    ],
  },
];
export const sectionLabels: Record<string, string> = {
  Explore: 'Avatars',
  MyGroups: 'My groups',
  Uploads: 'My uploads',
  Profile: 'My profile',
  Account: 'VRCove account',
  CoveProfile: 'Profile',
  Settings: 'Settings',
};
export const primaryNavigation = [
  { name: 'Home', label: 'Home', icon: HomeIcon },
  { name: 'Search', label: 'Search', icon: SearchIcon },
];
export const accountNavigation = [
  { name: 'Account', label: 'VRCove account', icon: KeyRound },
  { name: 'Settings', label: 'Settings', icon: SettingsIcon },
];
