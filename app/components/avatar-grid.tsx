import { ArrowUpRight, Bookmark } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import type { Avatar } from '../lib/avatar';
import { Picture, Platform } from './avatar-media';

/** Avatar cards that open the quick view, each with a bookmark toggle. Shows placeholders while loading. */
export default function AvatarGrid({ avatars, loading = false }: { avatars: Avatar[]; loading?: boolean }) {
  useLocale();
  const { saved, bookmark, openAvatar } = useApp();
  if (loading)
    return (
      <div className="avatar-grid skeleton-grid">
        {Array.from({ length: 8 }, (_, i) => (
          <div className="skeleton-card" key={i}>
            <div />
            <span />
            <small />
          </div>
        ))}
      </div>
    );
  return (
    <div className="avatar-grid">
      {avatars.map((a, i) => {
        const isSaved = saved.some((s) => s.id === a.id);
        return (
          <article className="avatar-card" key={a.id}>
            <button className="card-image" onClick={() => openAvatar(a)} aria-label={'View ' + a.name}>
              <Picture src={a.thumbnail} alt={a.name} loading={i < 4 ? 'eager' : 'lazy'} />
              <span className="card-overlay" />
              <Platform a={a} />
              <span className="view-avatar">
                {tr('View avatar')}
                <ArrowUpRight size={15} />
              </span>
            </button>
            <button
              className={'bookmark-button ' + (isSaved ? 'is-saved' : '')}
              onClick={() => bookmark(a)}
              aria-label={(isSaved ? 'Unbookmark ' : 'Bookmark ') + a.name}
            >
              <Bookmark size={15} fill={isSaved ? 'currentColor' : 'none'} />
            </button>
            <div className="card-caption">
              <button onClick={() => openAvatar(a)}>
                <h3>{a.name}</h3>
                <p>
                  {tr('by')} <span>{a.author}</span>
                </p>
              </button>
              <i
                className={'rating-dot ' + String(a.rating || 'Unknown').toLowerCase()}
                title={'PC performance: ' + (a.rating || 'Unknown')}
              />
            </div>
          </article>
        );
      })}
    </div>
  );
}
