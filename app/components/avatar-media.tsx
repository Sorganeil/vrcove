import { useState } from 'react';
import { UserRound, Monitor, Smartphone } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import type { Avatar } from '../lib/avatar';

// Avatar image through the image proxy, falling back to the thumbnail, then a placeholder.
export function Picture({ src, fallbackSrc, alt, ...props }: any) {
  useLocale();
  const sources = [...new Set<string>([src, fallbackSrc].filter(Boolean))];
  const requestKey = JSON.stringify(sources);
  const [attempt, setAttempt] = useState({ requestKey, index: 0 });
  const index = attempt.requestKey === requestKey ? attempt.index : 0;
  const current = sources[index];
  return current ? (
    <img
      key={requestKey + index}
      src={'/api/image?url=' + encodeURIComponent(current)}
      alt={alt}
      onError={() => setAttempt({ requestKey, index: index + 1 })}
      referrerPolicy="no-referrer"
      {...props}
    />
  ) : (
    <div className="image-fallback">
      <UserRound strokeWidth={0.8} />
      <span>{tr('Preview unavailable')}</span>
    </div>
  );
}
export function Platform({ a }: { a: Avatar }) {
  useLocale();
  return (
    <span className="platform">
      {a.pc && (
        <span>
          <Monitor size={11} />
          PC
        </span>
      )}
      {a.quest && (
        <span className="quest">
          <Smartphone size={11} />
          Quest
        </span>
      )}
      {!a.pc && !a.quest && <span>{tr('Platform unknown')}</span>}
    </span>
  );
}
