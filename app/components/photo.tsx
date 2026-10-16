import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { UserRound } from 'lucide-react';

export function Photo({ url, fallbackUrl, name, alt = '' }: any) {
  useLocale();
  const sources = [...new Set<string>([url, fallbackUrl].filter(Boolean))];
  const requestKey = JSON.stringify(sources);
  const [attempt, setAttempt] = useState({ requestKey, index: 0 });
  const index = attempt.requestKey === requestKey ? attempt.index : 0;
  const current = sources[index];
  if (current)
    return (
      <img
        key={requestKey + index}
        src={'/api/image?url=' + encodeURIComponent(current)}
        alt={alt}
        onError={() => setAttempt({ requestKey, index: index + 1 })}
        referrerPolicy="no-referrer"
        loading="lazy"
      />
    );
  return name ? (
    <div className="initial-fallback" aria-hidden="true">
      {[...String(name).trim()][0]?.toUpperCase() || '?'}
    </div>
  ) : (
    <div className="image-fallback">
      <UserRound strokeWidth={0.8} />
      <span>{tr('Preview unavailable')}</span>
    </div>
  );
}
