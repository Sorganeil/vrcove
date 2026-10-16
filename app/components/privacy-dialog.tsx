import { ShieldCheck } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import Modal from './modal';

/** What VRCove stores, shares and sends, in plain language. */
export default function PrivacyDialog({ close }: { close: () => void }) {
  useLocale();
  return (
    <Modal title={tr('Privacy & data')} close={close}>
      <div className="privacy-content">
        <ShieldCheck size={32} />
        <h2>{tr('Your privacy')}</h2>
        <p>
          {tr(
            'VRCove is an independent frontend for VRChat. It adds no ads, analytics, session replay, tracking pixels or third-party error reporting. No Amplitude, Google Analytics, Mixpanel or Sentry. Fonts and interface assets are served by this site.',
          )}
        </p>
        <h3>{tr('How this compares with VRChat')}</h3>
        <p>
          {tr('VRChat’s')}{' '}
          <a
            className="privacy-policy-link"
            href="https://hello.vrchat.com/cookie-policy"
            target="_blank"
            rel="noreferrer"
          >
            {tr('cookie policy')}
          </a>{' '}
          {tr(
            'lists analytics providers including Amplitude, Google and Mixpanel. VRCove leaves those integrations out of this web interface. Account requests still reach VRChat; this does not change tracking in the game or on VRChat’s own pages.',
          )}
        </p>
        <h3>{tr('When you connect')}</h3>
        <p>
          {tr(
            'Your password and verification code pass through this server to VRChat over HTTPS. They are used for that request and never intentionally written to disk, logs, or a database. This is direct API login, not OAuth.',
          )}
        </p>
        <p>
          {tr(
            'Session tokens are encrypted using AES-256-GCM inside an HttpOnly, Secure, SameSite=Strict browser cookie. The server decrypts them only to make your requests. Sessions expire after 2 weeks. Disconnect attempts to revoke the VRChat session and clears your cookie.',
          )}
        </p>
        <h3>{tr('What stays here')}</h3>
        <p>
          {tr(
            'The catalog stores public avatar metadata from the imported database. Private entries, avatar bundles, and account credentials are excluded. Bookmarks live in this browser. Favorites belong to your VRChat account.',
          )}
        </p>
        <h3>{tr('VRCove accounts')}</h3>
        <p>
          {tr(
            'A VRCove account is separate from VRChat: a username and an Argon2id password hash, with no email. Reviews are public and show only your VRCove username. Linked VRChat accounts are visible only to you and are stored encrypted. Choose a VRCove username unrelated to your VRChat names to reduce correlation by other people, including VRChat staff. This cannot guarantee anonymity or prevent VRChat account action. The server operator holds the key that can decrypt private links.',
          )}
        </p>
        <h3>{tr('Profiles and collections')}</h3>
        <p>
          {tr(
            'Your profile starts private. Making it public shares your bio, review history and public collections. Reviews remain public on the reviewed items even with a private profile. Collections start private unless you choose public; anyone with a public collection’s link can view it. Its owner is named only when their profile is public. Linked VRChat accounts never appear in these public views. Deleting your account removes your reviews, collections and links; older backups may retain copies.',
          )}
        </p>
        <h3>{tr('What leaves this site')}</h3>
        <p>
          {tr(
            'Account actions and image requests contact VRChat. Images are proxied so your browser does not contact image hosts directly. The hosting provider and VRChat still process traffic; VRCove cannot promise they keep no operational logs. External links open VRChat’s own website.',
          )}
        </p>
        <div className="privacy-note">
          <ShieldCheck size={19} />
          <p>
            {tr(
              'Necessary cookies keep your VRCove account signed in and hold your encrypted VRChat session. A deployment may also use a separate access gate. Never paste a VRChat auth cookie into this site.',
            )}
          </p>
        </div>
      </div>
    </Modal>
  );
}
