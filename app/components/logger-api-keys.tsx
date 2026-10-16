import { useEffect, useState } from 'react';
import { Copy, KeyRound, LoaderCircle, X } from 'lucide-react';
import { api } from '../lib/api';
import { formatDate, t as tr } from '../lib/i18n';

type LoggerKey = { id: number; vrchatId: string; expires: number; browser: string; device: string };
type Link = { vrchatId: string; displayName: string };
const expiry = (value: number) =>
  formatDate(new Date(value).toISOString(), { dateStyle: 'medium', timeStyle: 'short' });

export default function LoggerApiKeys({
  currentId,
  connected,
  links,
  notify,
}: {
  currentId?: string;
  connected: boolean;
  links: Link[];
  notify: (message: string) => void;
}) {
  const [keys, setKeys] = useState<LoggerKey[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [generated, setGenerated] = useState<{ id: number; key: string; expires: number } | null>(null);
  const load = async () => {
    const data = await api('/cove/logger-keys');
    setKeys(data.keys);
    setEnabled(data.enabled);
  };
  useEffect(() => {
    let active = true;
    api('/cove/logger-keys')
      .then((data) => {
        if (active) {
          setKeys(data.keys);
          setEnabled(data.enabled);
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);
  async function generate() {
    setBusy(true);
    setError('');
    setGenerated(null);
    try {
      const data = await api('/cove/logger-keys', { vrchatId: currentId });
      setGenerated({ id: data.id, key: data.key, expires: data.expires });
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function revoke(id: number) {
    setBusy(true);
    setError('');
    try {
      await api('/cove/logger-keys/revoke', { id });
      if (generated?.id === id) setGenerated(null);
      await load();
      notify(tr('API key revoked.'));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="cove-panel logger-api-keys" aria-labelledby="logger-api-keys-title">
      <div className="eyebrow">{tr('DESKTOP COMPANION')}</div>
      <h3 id="logger-api-keys-title">
        <KeyRound size={18} />
        {tr('Companion API keys')}
      </h3>
      <p>
        {tr(
          'Generate an API key for your active VRChat session, then paste it into the desktop companion’s API key field. The companion also needs its own VRChat login, made directly with VRChat, to check avatars before upload. The server then verifies every avatar again with this session.',
        )}
      </p>
      <p className="fineprint">
        {tr(
          'Enabling an API key stores your VRChat session credentials encrypted in the existing server-side session record. This lets the server fetch avatar metadata and verify submissions while your browser is closed. The desktop companion receives only the API key, never your VRChat credentials.',
        )}
      </p>
      <p className="fineprint">
        {tr(
          'The API key is tied to this session’s lifetime. It expires when the VRChat session expires or ends, including when you sign out or unlink the account. Reconnecting requires a new API key. Generating another key replaces the previous key for this session.',
        )}
      </p>
      {loading ? (
        <p role="status">{tr('Loading…')}</p>
      ) : (
        <>
          {!enabled && (
            <p className="fineprint">
              {tr('Desktop companion submissions are not enabled on this deployment.')}
            </p>
          )}
          {!connected && (
            <p className="fineprint">{tr('Connect a linked VRChat account to generate an API key.')}</p>
          )}
          <button
            className="standard-button"
            disabled={!enabled || !connected || !currentId || busy}
            onClick={generate}
          >
            {busy ? <LoaderCircle size={14} className="spin" /> : <KeyRound size={14} />}
            {tr('Generate API key')}
          </button>
          {generated && (
            <div className="logger-generated-key" role="status">
              <label htmlFor="logger-api-key">{tr('Copy this API key now. It is shown only once.')}</label>
              <textarea
                id="logger-api-key"
                readOnly
                value={generated.key}
                rows={2}
                spellCheck={false}
                autoComplete="off"
              />
              <small>
                {tr('Expires with your VRChat session: {expires}', { expires: expiry(generated.expires) })}
              </small>
              <div className="result-actions">
                <button
                  className="standard-button"
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(generated.key);
                      notify(tr('API key copied.'));
                    } catch {
                      setError(tr('Could not copy. Select and copy the API key manually.'));
                    }
                  }}
                >
                  <Copy size={14} />
                  {tr('Copy API key')}
                </button>
                <button className="standard-button" onClick={() => setGenerated(null)}>
                  <X size={14} />
                  {tr('Hide API key')}
                </button>
              </div>
            </div>
          )}
          {keys.length > 0 && (
            <ul className="logger-key-list">
              {keys.map((key) => (
                <li key={key.id}>
                  <div>
                    <strong>
                      {links.find((link) => link.vrchatId === key.vrchatId)?.displayName ||
                        tr('VRChat session')}
                      {key.browser ? ` · ${key.browser}` : ''}
                    </strong>
                    <small>{tr('Expires {expires}', { expires: expiry(key.expires) })}</small>
                  </div>
                  <button className="standard-button" disabled={busy} onClick={() => revoke(key.id)}>
                    {tr('Revoke API key')}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
