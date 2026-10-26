import { Activity, History, Images, Library, Upload, KeyRound, MonitorDown } from 'lucide-react';
import { desktop } from './platform';
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
  // The desktop app's own pages, kept on this computer.
  ...(desktop
    ? [
        {
          label: 'On this PC',
          items: [
            { name: 'History', label: 'History', icon: History },
            { name: 'Feed', label: 'Feed', icon: Activity },
            { name: 'Screenshots', label: 'Screenshots', icon: Images },
          ],
        },
      ]
    : []),
];
export const sectionLabels: Record<string, string> = {
  Explore: 'Avatars',
  MyGroups: 'My groups',
  Uploads: 'My uploads',
  Profile: 'My profile',
  Account: 'VRCove account',
  Companion: 'Desktop companion',
  Admin: 'Moderation',
  CoveProfile: 'Profile',
  Settings: 'Settings',
};
export const primaryNavigation = [
  { name: 'Home', label: 'Home', icon: HomeIcon },
  { name: 'Search', label: 'Search', icon: SearchIcon },
];
export const accountNavigation = [
  { name: 'Companion', label: 'Desktop companion', icon: MonitorDown },
  { name: 'Account', label: 'VRCove account', icon: KeyRound },
  { name: 'Settings', label: 'Settings', icon: SettingsIcon },
];
