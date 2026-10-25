import { useState } from 'react';
import { Search } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import AvatarGrid from '../components/avatar-grid';
import { SectionPage } from '../components/section-page';
import SearchBar, { SortMenu } from '../components/search-bar';

const sorts = [
  ['saved', 'Recently saved'],
  ['name', 'Name A–Z'],
  ['author', 'Creator A–Z'],
] as const;

/** Avatars bookmarked in this browser, newest first. */
export default function Bookmarks() {
  useLocale();
  const { saved } = useApp();
  const [q, setQ] = useState('');
  const [sort, setSort] = useState('saved');
  const needle = q.trim().toLowerCase();
  const list = saved.filter((a) => (a.name + ' ' + a.author + ' ' + a.id).toLowerCase().includes(needle));
  if (sort !== 'saved') {
    const key = (a: (typeof list)[number]) => (sort === 'name' ? a.name : a.author + '\n' + a.name);
    list.sort((a, b) => key(a).localeCompare(key(b), undefined, { sensitivity: 'base' }));
  }
  return (
    <SectionPage title="Bookmarks" subtitle="Saved on this device.">
      <SearchBar
        label="Search bookmarks"
        placeholder="Search avatars, creators, or an avatar ID…"
        value={q}
        onChange={setQ}
      />
      <div className="browse-bar">
        <span className="muted">{tr('{count} bookmarks', { count: list.length })}</span>
        <SortMenu label="Sort bookmarks" value={sort} options={sorts} onChange={setSort} />
      </div>
      {list.length ? (
        <AvatarGrid avatars={list} />
      ) : (
        <div className="empty-state">
          <Search size={30} />
          <h3>{tr(saved.length ? 'No bookmarks found' : 'No bookmarks yet')}</h3>
          <p>
            {tr(saved.length ? 'Try a different search.' : 'Tap the bookmark on any avatar to save it here.')}
          </p>
        </div>
      )}
    </SectionPage>
  );
}
