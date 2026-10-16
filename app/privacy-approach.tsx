import { t as tr, useLocale } from './i18n';
export default function PrivacyApproach() {
  useLocale();
  return (
    <section className="privacy-approach" aria-labelledby="privacy-approach-title">
      <h2 id="privacy-approach-title">{tr('Less tracking in your browser.')}</h2>
      <p>
        {tr(
          'VRCove leaves out the analytics and advertising integrations. You can browse the public avatar catalog without connecting a VRChat account.',
        )}
      </p>
      <div className="privacy-explanation">
        <div>
          <h3>{tr('What we leave out')}</h3>
          <ul>
            <li>{tr('No Amplitude, Google Analytics or Mixpanel')}</li>
            <li>{tr('No Sentry or third-party error reporting')}</li>
            <li>{tr('No ads, advertising pixels or session replay')}</li>
            <li>{tr('No server-side browsing or friend-location history')}</li>
          </ul>
        </div>
        <div>
          <h3>{tr('What still gets shared')}</h3>
          <p>
            {tr(
              'Your account requests and actions go to VRChat through VRCove. The server briefly handles your login and decrypts your session to make those requests.',
            )}
          </p>
          <p>
            {tr(
              'VRChat and the hosting provider still process traffic and may retain operational logs. VRCove does not change tracking inside the VRChat game or on external websites.',
            )}
          </p>
        </div>
      </div>
      <p className="privacy-source">
        {tr('VRChat’s')}{' '}
        <a href="https://hello.vrchat.com/cookie-policy" target="_blank" rel="noreferrer">
          {tr('published cookie policy')}
        </a>{' '}
        {tr(
          'lists analytics providers including Amplitude, Google and Mixpanel. VRCove does not include these integrations. This comparison concerns the web interface, not every VRChat service.',
        )}
      </p>
      <p>
        {tr(
          'VRCove reviews are public. Profiles start private; making one public shares your bio, reviews and public collections. Anyone with a public collection’s link can view it. Linked VRChat accounts stay private and encrypted, though the server operator can decrypt them. Choose an unrelated VRCove username to reduce correlation; this cannot guarantee anonymity.',
        )}
      </p>
      <p className="privacy-source">
        {tr(
          'Necessary cookies keep your VRCove account signed in and hold your encrypted VRChat session. A deployment may also use a separate access gate. Bookmarks and notes stay in this browser unless you export them.',
        )}
      </p>
    </section>
  );
}
