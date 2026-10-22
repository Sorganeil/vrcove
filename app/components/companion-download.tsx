import { useState } from 'react';
import { Download, MonitorDown, X } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';

export const companionReleaseUrl = 'https://github.com/Sorganeil/vrcove/releases/latest';

const dismissedKey = 'vrcove-companion-banner-dismissed';

/* Companion promo on Home and Avatar search. Dismissing hides it in this browser; the sidebar still links the page. */
export default function CompanionDownload() {
  useLocale();
  const { sectionLink } = useApp();
  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(dismissedKey) === '1';
    } catch {
      return false;
    }
  });
  if (dismissed) return null;
  function dismiss() {
    setDismissed(true);
    try {
      localStorage.setItem(dismissedKey, '1');
    } catch {
      // Without storage it stays hidden until the page reloads.
    }
  }
  return (
    <aside className="companion-download desktop-only" aria-label={tr('Desktop companion')}>
      <span className="companion-download-icon" aria-hidden="true">
        <MonitorDown size={22} />
      </span>
      <div>
        <h2>{tr('Want to help grow the avatar database?')}</h2>
        <p>
          {tr(
            'Install our desktop companion for Windows or Linux to contribute public avatar IDs discovered while you play VRChat.',
          )}
        </p>
        <p>
          {tr(
            'To upload, generate an API key on your Account page, then sign in to VRChat in the companion.',
          )}
        </p>
      </div>
      <a className="standard-button" {...sectionLink('Companion')}>
        <Download size={16} aria-hidden="true" />
        {tr('Get the companion')}
      </a>
      <button
        className="companion-dismiss"
        aria-label={tr('Dismiss')}
        title={tr('Dismiss')}
        onClick={dismiss}
      >
        <X size={16} />
      </button>
    </aside>
  );
}
