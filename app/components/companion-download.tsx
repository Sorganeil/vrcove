import { Download, MonitorDown } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';

export const companionReleaseUrl = 'https://github.com/Sorganeil/vrcove/releases/latest';

export default function CompanionDownload() {
  useLocale();
  const { sectionLink } = useApp();
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
    </aside>
  );
}
