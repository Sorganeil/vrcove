import { desktop } from '../lib/platform';
import { Compass } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';

export default function SiteFooter() {
  useLocale();
  const { sectionLink, openPrivacy } = useApp();
  return (
    <footer>
      <div className="footer-brand">
        <Compass size={17} />
        VRCove<span>{tr('Independent VRChat frontend')}</span>
      </div>
      <div>
        <a href="https://github.com/Sorganeil/vrcove" target="_blank" rel="noopener noreferrer">
          GitHub
        </a>
        {!desktop && <a {...sectionLink('Companion')}>{tr('Desktop app')}</a>}
        <a {...sectionLink('About')}>{tr('About')}</a>
        <a {...sectionLink('Donations')}>{tr('Donations')}</a>
        <button onClick={openPrivacy}>{tr('Privacy & data')}</button>
        <span>{tr('Independent. Not affiliated with VRChat.')}</span>
      </div>
    </footer>
  );
}
