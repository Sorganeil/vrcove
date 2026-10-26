import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { companionReleaseUrl } from '../components/companion-download';
import {
  ArrowUpRight,
  Code,
  Download,
  FileSearch,
  History,
  MonitorDown,
  ShieldCheck,
  Wrench,
} from 'lucide-react';

const features = [
  {
    icon: ShieldCheck,
    title: 'Straight to VRChat',
    points: [
      'Your VRChat password and session go from your computer to VRChat and never pass through VRCove’s server, so you don’t have to trust us with them.',
      'The session is kept in a file encrypted with a key in Windows Credential Manager or your Linux keyring.',
    ],
  },
  {
    icon: FileSearch,
    title: 'Avatar logger built in',
    points: [
      'Finds public avatar IDs in VRChat’s analytics cache while you play and adds new ones to VRCove’s search. No game injection or memory access.',
      'Each avatar is checked with your VRChat sign-in, then again by the server. Uploading needs an API key from your Account page.',
    ],
  },
  {
    icon: History,
    title: 'Your history, on your computer',
    points: [
      'Instances you joined and the people you met, read from VRChat’s logs, a friend activity feed, and your screenshots with the world and people.',
      'Notifications keep working with the window closed. All of it stays on your computer.',
    ],
  },
  {
    icon: Wrench,
    title: 'Tools',
    points: [
      'Username checker, OSC chatbox and Unity package extractor.',
      'Updates are offered in the app and installed only after their checksums match the release.',
    ],
  },
];

/* VRCove Desktop: what it does and how to set it up. Downloads come from GitHub. */
export default function Companion() {
  useLocale();
  const { sectionLink } = useApp();
  return (
    <section className="about-page companion-page" aria-labelledby="companion-title">
      <div className="about-intro">
        <span className="eyebrow">
          <MonitorDown size={15} />
          {tr('VRCOVE DESKTOP')}
        </span>
        <h1 id="companion-title">{tr('VRCove on your desktop.')}</h1>
        <p>
          {tr(
            'The full VRCove app for Windows and Linux. It looks and works like this website, but talks to VRChat directly from your computer. A VRCove account is optional.',
          )}
        </p>
        <div className="companion-download-actions">
          <a
            className="standard-button primary"
            href={companionReleaseUrl}
            target="_blank"
            rel="noopener noreferrer"
          >
            <Download size={16} aria-hidden="true" />
            {tr('Download for Windows or Linux')}
          </a>
          <span>{tr('Free and open source · downloads from GitHub')}</span>
        </div>
      </div>
      <article className="about-feature companion-setup">
        <h2>{tr('Setup')}</h2>
        <ol>
          <li>
            {tr(
              'Download the Windows .zip or Linux .tar.gz from the latest release, extract it, and run vrcove-desktop.',
            )}
          </li>
          <li>
            {tr(
              'Sign in to VRChat in the app. Enter your authenticator, email or recovery code if VRChat asks.',
            )}
          </li>
          <li>
            {tr(
              'Optional, to add avatars to the search: on VRCove, sign in, connect VRChat, and generate an API key on your Account page. Paste it into Avatar logger in the app.',
            )}{' '}
            <a {...sectionLink('Account')}>{tr('Open Account page')}</a>
          </li>
        </ol>
        <p>
          {tr(
            'VRCove Desktop replaces the old VRCove Logger, which is retired. It picks up the logger’s settings and queue automatically.',
          )}
        </p>
      </article>
      <div className="about-grid">
        {features.map((f) => (
          <article className="about-feature" key={f.title}>
            <h2>
              <f.icon size={18} />
              {tr(f.title)}
            </h2>
            <ul>
              {f.points.map((p) => (
                <li key={p}>{tr(p)}</li>
              ))}
            </ul>
          </article>
        ))}
      </div>
      <article className="about-feature about-open">
        <h2>
          <Code size={18} />
          {tr('Open source')}
        </h2>
        <p>
          {tr(
            'Built with Go and Wails, using the same code as this website. Linux needs GTK 4, WebKitGTK and a tray host such as KDE Plasma to run in the background.',
          )}
        </p>
        <a
          className="standard-button"
          href="https://github.com/Sorganeil/vrcove/tree/main/companion"
          target="_blank"
          rel="noopener noreferrer"
        >
          {tr('View source')}
          <ArrowUpRight size={15} />
        </a>
      </article>
    </section>
  );
}
