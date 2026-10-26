import { useEffect, useState } from 'react';
import { Download, ExternalLink, LoaderCircle, RefreshCw } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { desktop } from '../lib/platform';

type UpdateStatus = {
  currentVersion: string;
  latestVersion?: string;
  releaseUrl?: string;
  assetName?: string;
  available: boolean;
  checking: boolean;
  installing: boolean;
  installed: boolean;
  checkedAt?: number;
  error?: string;
};

const bundledVersion = (import.meta as { env?: { APP_VERSION?: string } }).env?.APP_VERSION || 'dev';

export default function DesktopUpdateSettings({ notify }: { notify: (message: string) => void }) {
  useLocale();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [autostart, setAutostart] = useState(false);
  const [busy, setBusy] = useState<'check' | 'install' | 'autostart' | ''>('');
  const [error, setError] = useState('');

  useEffect(() => {
    if (!desktop) return;
    let alive = true;
    api('/desktop/update')
      .then((s: UpdateStatus) => {
        if (alive) setStatus(s);
      })
      .catch(() => {});
    api('/desktop/autostart')
      .then((s: { enabled?: boolean }) => {
        if (alive && typeof s?.enabled === 'boolean') setAutostart(s.enabled);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  if (!desktop) return null;

  const currentVersion = status?.currentVersion || bundledVersion;
  const latestVersion = status?.latestVersion || currentVersion;

  async function toggleAutostart(enabled: boolean) {
    setBusy('autostart');
    setError('');
    try {
      const next: { enabled: boolean } = await api('/desktop/autostart', { enabled });
      setAutostart(next.enabled);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  async function runAction(action: 'check' | 'install') {
    setBusy(action);
    setError('');
    try {
      const next: UpdateStatus = await api('/desktop/update', { action });
      setStatus(next);
      if (action === 'install' && next.installed) {
        notify(
          tr('Update {version} is installed. Restart VRCove to use the new version.', {
            version: next.latestVersion || latestVersion,
          }),
        );
      } else if (action === 'check' && !next.available && !next.installed) {
        notify(tr('VRCove is up to date ({version}).', { version: next.currentVersion || currentVersion }));
      }
    } catch (e) {
      const message = (e as Error).message;
      setError(message);
      api('/desktop/update')
        .then(setStatus)
        .catch(() => {});
    } finally {
      setBusy('');
    }
  }

  const activeError = error || status?.error || '';

  return (
    <section className="dashboard-panel desktop-updates" aria-labelledby="desktop-updates-title">
      <div className="panel-title">
        <h2 id="desktop-updates-title">{tr('App updates')}</h2>
        <Download size={16} />
      </div>
      <div className="settings-content">
        <p>
          {tr(
            'Checks GitHub Releases for new VRCove Desktop builds, verifies the SHA-256 checksum, and replaces the app binary on this computer.',
          )}
        </p>
        <label className="cove-switch">
          <input
            type="checkbox"
            checked={autostart}
            disabled={busy === 'autostart'}
            onChange={(e) => void toggleAutostart(e.target.checked)}
          />
          <span>{tr('Start VRCove when I sign in to my computer')}</span>
        </label>
        <p className="fineprint">{tr('Current version: {version}', { version: currentVersion })}</p>
        {status?.installed && (
          <p role="status">
            {tr('Update {version} is installed. Restart VRCove to use the new version.', {
              version: latestVersion,
            })}
          </p>
        )}
        {!status?.installed && status?.available && (
          <p role="status">{tr('Update {version} is available.', { version: latestVersion })}</p>
        )}
        {!status?.installed && !status?.available && !!status?.checkedAt && !activeError && (
          <p className="fineprint" role="status">
            {tr('VRCove is up to date ({version}).', { version: currentVersion })}
          </p>
        )}
        {activeError && (
          <p className="form-error" role="alert">
            {tr(activeError)}
          </p>
        )}
        <div className="settings-actions">
          <button
            className="standard-button"
            disabled={busy !== '' || !!status?.checking || !!status?.installing}
            onClick={() => void runAction('check')}
          >
            {busy === 'check' || status?.checking ? (
              <LoaderCircle size={14} className="spin" />
            ) : (
              <RefreshCw size={14} />
            )}
            {tr(busy === 'check' || status?.checking ? 'Checking for updates...' : 'Check for updates')}
          </button>
          {status?.available && !status?.installed && (
            <button
              className="standard-button primary"
              disabled={busy !== '' || !!status?.checking || !!status?.installing}
              onClick={() => void runAction('install')}
            >
              {busy === 'install' || status?.installing ? (
                <LoaderCircle size={14} className="spin" />
              ) : (
                <Download size={14} />
              )}
              {tr(busy === 'install' || status?.installing ? 'Installing update...' : 'Install update')}
            </button>
          )}
          {status?.releaseUrl && /^https:\/\//.test(status.releaseUrl) && (
            <a className="text-link" href={status.releaseUrl} target="_blank" rel="noopener noreferrer">
              {tr('View release on GitHub')}
              <ExternalLink size={13} />
            </a>
          )}
        </div>
      </div>
    </section>
  );
}
