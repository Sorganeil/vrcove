import { t as tr, useLocale } from '../lib/i18n';
import {
  ShieldCheck,
  Search,
  Smartphone,
  Download,
  Star,
  Users,
  Code,
  Info,
  ArrowUpRight,
  Heart,
} from 'lucide-react';

const features = [
  {
    icon: ShieldCheck,
    title: 'Private by default',
    points: [
      'No ads, analytics or tracking scripts.',
      'Images are proxied, so your browser never contacts VRChat’s image hosts.',
      'Linked VRChat sessions are stored encrypted.',
    ],
  },
  {
    icon: Search,
    title: 'Avatar search',
    points: [
      'Search our large public avatar database without linking VRChat.',
      'Filter by PC or Quest and by performance rank.',
      'Bookmark avatars, or switch to them once VRChat is linked.',
    ],
  },
  {
    icon: Smartphone,
    title: 'Mobile compatible',
    points: [
      'Works on Android, iPhone, iPad and desktop.',
      'Play on Quest without a PC? Manage your friends and avatars from your phone.',
    ],
  },
  {
    icon: Download,
    title: 'Install as an app',
    points: [
      'No Electron, .NET runtime, JVM, bundled Chromium or webview.',
      'Takes almost no storage and updates itself with the site.',
      'Use Install app in the menu, or Add to Home Screen on iPhone and iPad.',
    ],
  },
  {
    icon: Star,
    title: 'Reviews and collections',
    points: [
      'Rate and review users, worlds, avatars and groups.',
      'Group avatars and worlds into collections, private or shared by link.',
      'Profiles start private.',
    ],
  },
  {
    icon: Users,
    title: 'Your VRChat account',
    points: [
      'Friends, friend requests, favorites, notifications and your current avatar.',
      'See every avatar and world you’ve uploaded, including private ones.',
    ],
  },
];

/* About VRCove: what it is and what it does. */
export default function About({ onPrivacy, onDonate }: { onPrivacy: () => void; onDonate: () => void }) {
  useLocale();
  return (
    <section className="about-page" aria-labelledby="about-title">
      <div className="about-intro">
        <span className="eyebrow">
          <Info size={15} />
          {tr('ABOUT VRCOVE')}
        </span>
        <h1 id="about-title">{tr('VRChat in your browser, without the tracking.')}</h1>
        <p>
          {tr(
            'VRCove is an independent, privacy-focused VRChat web client with avatar search built in. Use it on your phone or PC, with nothing to install.',
          )}
        </p>
      </div>
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
      <div className="about-footer">
        <article className="about-feature about-open">
          <h2>
            <Code size={18} />
            {tr('Open source')}
          </h2>
          <p>{tr('VRCove is MIT licensed. Anyone can read, check or improve the code.')}</p>
          <a
            className="standard-button"
            href="https://github.com/Sorganeil/vrcove"
            target="_blank"
            rel="noopener noreferrer"
          >
            {tr('View on GitHub')}
            <ArrowUpRight size={15} />
          </a>
        </article>
        <div className="about-actions">
          <button className="standard-button" onClick={onPrivacy}>
            <ShieldCheck size={15} />
            {tr('Privacy & data')}
          </button>
          <button className="standard-button" onClick={onDonate}>
            <Heart size={15} />
            {tr('Support VRCove')}
          </button>
        </div>
      </div>
    </section>
  );
}
