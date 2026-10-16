import { ArrowUpRight, Users } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { Picture } from './avatar-media';
import { detailLink } from './detail-links';
import type { WorldSummary } from '../lib/types';

/** World cards that open the world quick view. */
export default function WorldGrid({ worlds }: { worlds: WorldSummary[] }) {
  useLocale();
  return (
    <div className="world-grid">
      {worlds.map((w) => (
        <a className="world-card" key={w.favoriteId || w.id} {...detailLink('world', w.id)}>
          <div>
            <Picture src={w.thumbnailImageUrl || w.imageUrl} alt={w.name} />
            <span>
              <Users size={12} />
              {w.occupants ?? '?'} {tr('online')}
            </span>
          </div>
          <h3>
            {w.name}
            <ArrowUpRight size={17} />
          </h3>
          <p>
            {tr('by')} {w.authorName}
          </p>
        </a>
      ))}
    </div>
  );
}
