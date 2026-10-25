import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { companionReleaseUrl } from '../components/companion-download';
import {
  ArrowUpRight,
  Code,
  Download,
  FileSearch,
  Gauge,
  KeyRound,
  MonitorDown,
  ShieldCheck,
} from 'lucide-react';

const features = [
  {
    icon: FileSearch,
    title: 'Reads VRChat’s analytics cache',
    points: [
      'Finds avatar IDs in amplitude.cache while you play. No game injection or memory access.',
      'The file is only read. VRCove receives avatar IDs, never the rest of your analytics.',
    ],
  },
  {
    icon: ShieldCheck,
    title: 'Checked twice',
    points: [
      'The companion checks each new avatar with your VRChat login and skips private or invalid ones.',
      'The server then verifies every avatar again on its own. Avatars already in the database are skipped.',
    ],
  },
  {
    icon: KeyRound,
    title: 'Your credentials',
    points: [
      'Your VRChat password goes only to VRChat and is never saved.',
      'The VRChat session is stored in Windows Credential Manager or your Linux keyring.',
      'The API key expires when its VRChat session on VRCove ends. Revoke it any time on your Account page.',
    ],
  },
  {
    icon: Gauge,
    title: 'Gentle on rate limits',
    points: [
      'Uploads up to 40 batches of five avatars per ten minutes, paced automatically.',
      'If VRChat or VRCove asks it to wait, it shows when it will retry and keeps collecting.',
    ],
  },
];

/* The desktop companion: what it does and how to set it up. Downloads come from GitHub. */
export default function Companion() {
  useLocale();
  const { sectionLink } = useApp();
  return (
    <section className="about-page companion-page" aria-labelledby="companion-title">
      <div className="about-intro">
        <span className="eyebrow">
          <MonitorDown size={15} />
          {tr('DESKTOP COMPANION')}
        </span>
        <h1 id="companion-title">{tr('Help grow the avatar database.')}</h1>
        <p>
          {tr(
            'A small native app for Windows and Linux that collects public avatar IDs while you play VRChat and adds new ones to VRCove’s search.',
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
              'Download the Windows .zip or Linux .tar.gz from the latest release, extract it, and run vrcove-logger.',
            )}
          </li>
          <li>
            {tr(
              'On VRCove, sign in, connect VRChat, and generate an API key on your Account page. Paste it into the companion’s Settings.',
            )}{' '}
            <a {...sectionLink('Account')}>{tr('Open Account page')}</a>
          </li>
          <li>
            {tr(
              'In the companion’s Settings, sign in to VRChat. Enter your authenticator, email or recovery code if VRChat asks.',
            )}
          </li>
          <li>
            {tr('Press Start, then play VRChat. Closing the window keeps it collecting in the system tray.')}
          </li>
        </ol>
        <p>
          {tr(
            'Uploads need both logins. The VRChat login checks avatars on your PC; the API key lets VRCove verify them again with your encrypted session on the server.',
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
      <article className="about-feature">
        <h2>
          <ShieldCheck size={18} />
          {tr('Coming next: VRCove Desktop')}
        </h2>
        <p>
          {tr(
            'A full VRCove app for Windows and Linux is in testing. It looks and works like this website, with the logger built in, but it talks to VRChat directly from your computer. Your VRChat password and session never pass through VRCove’s server, so you don’t have to trust us with them. A VRCove account is optional; avatar search, reviews and collections still come from vrcove.st.',
          )}
        </p>
      </article>
      <article className="about-feature about-open">
        <h2>
          <Code size={18} />
          {tr('Small and native')}
        </h2>
        <p>
          {tr(
            'Built with Go and Gio. No browser, Electron or runtime to install. Linux needs a tray host such as KDE Plasma to run in the background.',
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
