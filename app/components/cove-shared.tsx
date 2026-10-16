import { entityPath, covePath, appLink, openQuickView, type EntityKind } from '../lib/routes';
import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { Library, Lock, ChevronLeft } from 'lucide-react';
import { day } from '../lib/dates';

export type CollectionKind = 'avatar' | 'world';
/** What the collection form submits. */
export type CollectionInput = { kind: CollectionKind; name: string; description: string; public: boolean };

export type Summary = {
  id: string;
  kind: CollectionKind;
  name: string;
  description: string;
  public: boolean;
  updated: string;
  count: number;
  contains: boolean;
  preview: string[];
};

export const kindLabel: Record<string, string> = {
  user: 'User',
  world: 'World',
  avatar: 'Avatar',
  group: 'Group',
};

/* Where a reviewed or collected VRChat item opens in this app. */
export const targetHref = (kind: string, id: string) => entityPath(kind as EntityKind, id);

// A plain click opens the quick view, as elsewhere; modified clicks open the full page.
export const targetLink = (kind: string, id: string) => ({
  href: targetHref(kind, id),
  onClick: (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
    e.preventDefault();
    openQuickView(kind as EntityKind, id);
  },
});

export function Thumb({ url, alt = '' }: { url?: string; alt?: string }) {
  useLocale();
  const [fail, setFail] = useState(false);
  return url && !fail ? (
    <img
      src={'/api/image?url=' + encodeURIComponent(url)}
      alt={alt}
      onError={() => setFail(true)}
      referrerPolicy="no-referrer"
      loading="lazy"
    />
  ) : (
    <div className="image-fallback">
      <Library strokeWidth={0.8} />
    </div>
  );
}

export function Back() {
  useLocale();
  return (
    <div className="detail-toolbar">
      <button className="text-link" onClick={() => window.history.back()}>
        <ChevronLeft size={14} />
        {tr('Back')}
      </button>
    </div>
  );
}

export function CollectionCard({ c }: { c: Summary }) {
  useLocale();
  return (
    <a className="entity-card collection-card" {...appLink(covePath('collection', c.id))}>
      <div className={'entity-card-image wide collection-mosaic n' + Math.min(c.preview.length, 4)}>
        {c.preview.length ? (
          c.preview.slice(0, 4).map((u, i) => <Thumb key={i} url={u} />)
        ) : (
          <div className="image-fallback">
            <Library strokeWidth={0.8} />
          </div>
        )}
      </div>
      <strong>
        {c.name}
        {!c.public && <Lock size={12} aria-label={tr('Private')} />}
      </strong>
      <small>
        {tr(c.count === 1 ? '1 item' : '{count} items', { count: c.count })} {tr('· updated')}{' '}
        {day(c.updated)}
      </small>
    </a>
  );
}
