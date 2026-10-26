import { useState } from 'react';
import { Download, MonitorDown, X } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { desktop } from '../lib/platform';

export const companionReleaseUrl = 'https://github.com/Sorganeil/vrcove/releases/latest';

// A new key, so people who hid the old logger banner see the desktop app once.
const dismissedKey = 'vrcove-desktop-banner-dismissed';

/* VRCove Desktop promo on Home, and on Avatar search as a way to help grow the catalog. Dismissing hides it in this browser; the sidebar still links the page. */
export default function CompanionDownload({ contribute = false }: { contribute?: boolean }) {
  useLocale();
  const { sectionLink } = useApp();
  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(dismissedKey) === '1';
    } catch {
      return false;
    }
  });
  // The desktop app is already installed, so it never shows its own download.
  if (dismissed || desktop) return null;
  function dismiss() {
    setDismissed(true);
    try {
      localStorage.setItem(dismissedKey, '1');
    } catch {
      // Without storage it stays hidden until the page reloads.
    }
  }
  return (
    <aside className="companion-download desktop-only" aria-label={tr('VRCove Desktop')}>
      <span className="companion-download-icon" aria-hidden="true">
        <MonitorDown size={22} />
      </span>
      <div>
        <h2>{tr(contribute ? 'Want to help grow the avatar database?' : 'VRCove Desktop is here')}</h2>
        <p>
          {tr(
            contribute
              ? 'Install VRCove Desktop for Windows or Linux to contribute public avatar IDs discovered while you play VRChat.'
              : 'Use VRCove as an app on Windows or Linux. It talks to VRChat directly from your computer, keeps your history, and has an avatar logger built in.',
          )}
        </p>
      </div>
      <a className="standard-button" {...sectionLink('Companion')}>
        <Download size={16} aria-hidden="true" />
        {tr('Get VRCove Desktop')}
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
