import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { LoaderCircle, Copy, Braces, Download } from 'lucide-react';
import { RawKind } from './detail-links';
import { useLoad } from '../lib/use-load';
import type { VRChatUser } from '../lib/types';

/* Full, unfiltered VRChat JSON for an object, fetched only when opened. `local` adds another record to inspect (e.g. the catalog snapshot). */
export function RawJson({
  kind,
  id,
  user,
  notify,
  local,
  localLabel = 'Our database',
}: {
  kind: RawKind;
  id: string;
  user: VRChatUser | null;
  notify: (s: string) => void;
  local?: unknown;
  localLabel?: string;
}) {
  useLocale();
  const [open, setOpen] = useState(false),
    [tab, setTab] = useState<'live' | 'local'>(user ? 'live' : 'local');
  const live = useLoad(
    open && user && tab === 'live' ? '/account/raw?kind=' + kind + '&id=' + encodeURIComponent(id) : null,
    user,
  );
  const value = tab === 'local' ? local : live.data;
  const text = value === undefined || value === null ? '' : JSON.stringify(value, null, 2);
  function download() {
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json' })),
      a = document.createElement('a');
    a.href = url;
    a.download = id + (tab === 'local' ? '.catalog' : '') + '.json';
    a.click();
    URL.revokeObjectURL(url);
  }
  return (
    <details className="raw-json" onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}>
      <summary>
        {kind === 'avatar' ? (
          <>
            <Download size={14} />
            {tr('View metadata')}
          </>
        ) : (
          <>
            <Braces size={14} />
            {tr('View metadata')}
          </>
        )}
      </summary>
      {open && (
        <div className="raw-json-body">
          <div className="raw-json-bar">
            {local !== undefined && (
              <div className="chips">
                <button className={tab === 'live' ? 'chosen' : ''} onClick={() => setTab('live')}>
                  {tr('Live VRChat')}
                </button>
                <button className={tab === 'local' ? 'chosen' : ''} onClick={() => setTab('local')}>
                  {localLabel}
                </button>
              </div>
            )}
            <span />
            {text && (
              <>
                <button
                  className="standard-button"
                  onClick={() =>
                    navigator.clipboard
                      .writeText(text)
                      .then(() => notify(tr('JSON copied')))
                      .catch(() => notify(tr('Clipboard access is unavailable.')))
                  }
                >
                  <Copy size={13} />
                  {tr('Copy')}
                </button>
                <button className="standard-button" onClick={download}>
                  <Download size={13} />
                  {tr('Download')}
                </button>
              </>
            )}
          </div>
          {tab === 'live' && !user ? (
            <p className="fineprint">{tr('Connect your VRChat account to load the live record.')}</p>
          ) : tab === 'live' && live.error ? (
            <p className="fineprint">{live.error}</p>
          ) : text ? (
            <pre>{text}</pre>
          ) : (
            <div className="loading-inline">
              <LoaderCircle size={14} className="spin" />
              {tr('Loading…')}
            </div>
          )}
        </div>
      )}
    </details>
  );
}
