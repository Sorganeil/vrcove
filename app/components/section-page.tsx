import type { ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import VRChatGate from './vrchat-gate';

/** Heading and body shared by the catalog and VRChat list pages. `title` and `subtitle` are untranslated. */
export function SectionPage({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  useLocale();
  return (
    <section className="catalog-section">
      <div className="section-heading">
        <div>
          <h2>{tr(title)}</h2>
          <p>{tr(subtitle)}</p>
        </div>
        <span className="section-index"></span>
      </div>
      {children}
    </section>
  );
}

/** Shows the VRChat connection prompt instead of `children` until VRChat is connected. */
export function RequireVRChat({ section, children }: { section: string; children: ReactNode }) {
  const { user, coveAccount, requestConnection } = useApp();
  if (!user)
    return (
      <VRChatGate
        coveAccount={coveAccount}
        section={tr(section)}
        onConnect={() => requestConnection()}
        size={34}
      />
    );
  return <>{children}</>;
}

/** An error with a retry button. */
export function ErrorBanner({ error, retry }: { error: string; retry: () => void }) {
  useLocale();
  if (!error) return null;
  return (
    <div className="error-banner" role="alert">
      {error}
      <button onClick={retry}>
        {tr('Try again')}
        <RefreshCw size={14} />
      </button>
    </div>
  );
}
