import { Download, MonitorDown } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';

export const companionReleaseUrl = 'https://github.com/Sorganeil/vrcove/releases/latest';

export default function CompanionDownload() {
  useLocale();
  return (
    <aside className="companion-download" aria-label={tr('Desktop companion')}>
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
      </div>
      <a className="standard-button" href={companionReleaseUrl} target="_blank" rel="noopener noreferrer">
        <Download size={16} aria-hidden="true" />
        {tr('Download companion')}
      </a>
    </aside>
  );
}
