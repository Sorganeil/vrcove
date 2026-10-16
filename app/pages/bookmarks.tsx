import { useState } from 'react';
import { Search, X } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import AvatarGrid from '../components/avatar-grid';
import { SectionPage } from '../components/section-page';

/** Avatars bookmarked in this browser. */
export default function Bookmarks() {
  useLocale();
  const { saved } = useApp();
  const [q, setQ] = useState('');
  const list = saved.filter((a) => (a.name + ' ' + a.author).toLowerCase().includes(q.toLowerCase()));
  return (
    <SectionPage title="Bookmarks" subtitle="Saved on this device.">
      <div className="search-row">
        <div className="search-input">
          <Search size={19} />
          <input
            aria-label={tr('Search avatars')}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={tr('Search avatars, creators, or an avatar ID…')}
          />
          {q ? (
            <button aria-label={tr('Clear search')} onClick={() => setQ('')}>
              <X size={16} />
            </button>
          ) : (
            <kbd>/</kbd>
          )}
        </div>
      </div>
      <div className="results-line">
        <span>{tr('{count} bookmarks', { count: list.length })}</span>
        <span>{tr('ON THIS DEVICE')}</span>
      </div>
      {list.length ? (
        <AvatarGrid avatars={list} />
      ) : (
        <div className="empty-state">
          <Search size={30} />
          <h3>{tr('No bookmarks yet')}</h3>
          <p>{tr('Tap the bookmark on any avatar to save it here.')}</p>
        </div>
      )}
    </SectionPage>
  );
}
