import { useEffect, useState } from 'react';
import { Archive, FolderOpen, LoaderCircle, MessageSquare, Play, Send, Square } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { SectionPage } from '../components/section-page';
import { UsernameChecker } from '../components/username-checker';

type Settings = { template: string; status: string; port: number; interval: number; sound: boolean };
type ChatboxState = { settings: Settings; automatic: boolean; error: string };
const defaults: Settings = { template: '', status: '', port: 9000, interval: 10, sound: false };

function preview(s: Settings, now: Date) {
  const clock = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  const date = [
    now.getFullYear(),
    String(now.getMonth() + 1).padStart(2, '0'),
    String(now.getDate()).padStart(2, '0'),
  ].join('-');
  return s.template.replace(
    /\{(time|date|status)\}/g,
    (_, key: string) => ({ time: clock, date, status: s.status })[key] || '',
  );
}

export default function DesktopTools() {
  useLocale();
  const { user } = useApp();
  const [settings, setSettings] = useState(defaults);
  const [remote, setRemote] = useState<ChatboxState | null>(null);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState('');
  const [metadata, setMetadata] = useState(true);
  const [extracting, setExtracting] = useState(false);
  const [extraction, setExtraction] = useState<{ folder: string; files: number } | null>(null);
  const [extractError, setExtractError] = useState('');
  const [now, setNow] = useState(new Date());
  useEffect(() => {
    let alive = true;
    api('/desktop/tools/chatbox')
      .then((d: ChatboxState) => {
        if (alive) {
          setSettings(d.settings);
          setRemote(d);
        }
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);
  useEffect(() => {
    if (!remote?.automatic) return;
    const timer = window.setInterval(() => {
      api('/desktop/tools/chatbox')
        .then(setRemote)
        .catch((e) => setError(e.message));
    }, 3000);
    return () => clearInterval(timer);
  }, [remote?.automatic]);
  const text = preview(settings, now);
  const valid = !!text.trim() && text.length <= 144;
  async function act(action: string) {
    setBusy(action);
    setError('');
    setMessage('');
    try {
      const d: ChatboxState = await api('/desktop/tools/chatbox', { ...settings, action });
      setRemote(d);
      setMessage(
        tr(
          action === 'save'
            ? 'Chatbox settings saved'
            : action === 'stop'
              ? 'Automatic messages stopped'
              : 'Sent to VRChat',
        ),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  async function extract() {
    setExtracting(true);
    setExtractError('');
    setExtraction(null);
    try {
      const result = await api('/desktop/tools/extract', { metadata });
      if (!result.cancelled) setExtraction(result);
    } catch (e) {
      setExtractError((e as Error).message);
    } finally {
      setExtracting(false);
    }
  }
  return (
    <SectionPage title="Tools" subtitle="Small tools for VRChat, running on this computer.">
      <div className="desktop-tools-grid">
        <UsernameChecker />
        <section className="dashboard-panel desktop-tool login-content" aria-labelledby="chatbox-title">
          <div className="desktop-tool-heading">
            <MessageSquare size={20} />
            <div>
              <h3 id="chatbox-title">{tr('OSC chatbox')}</h3>
              <p>{tr('Send text to your chatbox in VRChat.')}</p>
            </div>
            <span className={'tool-state' + (remote?.automatic ? ' active' : '')}>
              {tr(remote?.automatic ? 'Automatic' : 'Manual')}
            </span>
          </div>
          <p className="fineprint">
            {tr(
              'In VRChat, open the Action Menu, then OSC, then Enabled. Messages go only to VRChat on this PC.',
            )}
          </p>
          <div className="result-actions tool-presets" aria-label={tr('Presets')}>
            <button
              className="standard-button"
              disabled={!!remote?.automatic}
              onClick={() => setSettings({ ...settings, template: '{time}' })}
            >
              {tr('Time')}
            </button>
            <button
              className="standard-button"
              disabled={!!remote?.automatic}
              onClick={() => setSettings({ ...settings, template: tr('AFK: Back soon') })}
            >
              AFK
            </button>
            <button
              className="standard-button"
              disabled={!!remote?.automatic || !user?.statusDescription}
              onClick={() =>
                setSettings({ ...settings, template: '{status}', status: user?.statusDescription || '' })
              }
            >
              {tr('My VRChat status')}
            </button>
          </div>
          <div className="tool-message-field">
            <label htmlFor="chatbox-message">{tr('Message')}</label>
            <textarea
              id="chatbox-message"
              rows={3}
              maxLength={2048}
              value={settings.template}
              disabled={!!remote?.automatic}
              onChange={(e) => setSettings({ ...settings, template: e.target.value })}
              placeholder={tr('Write a message, or use a preset.')}
            />
          </div>
          <small className="fineprint">
            {tr('Use {time}, {date} or {status}. The clock updates with each message.')}
          </small>
          {settings.template.includes('{status}') && (
            <label>
              {tr('Status text')}
              <input
                value={settings.status}
                disabled={!!remote?.automatic}
                onChange={(e) => setSettings({ ...settings, status: e.target.value })}
              />
            </label>
          )}
          <div className="chatbox-preview" aria-label={tr('Message preview')}>
            <span>{text || tr('Your message appears here')}</span>
            <small className={text.length > 144 ? 'danger-text' : ''}>{text.length}/144</small>
          </div>
          <details className="tool-options">
            <summary>{tr('Chatbox options')}</summary>
            <div className="tool-option-fields">
              <label>
                {tr('OSC port')}
                <input
                  type="number"
                  min={1024}
                  max={65535}
                  disabled={!!remote?.automatic}
                  value={settings.port}
                  onChange={(e) => setSettings({ ...settings, port: Number(e.target.value) })}
                />
              </label>
              <label>
                {tr('Repeat every (seconds)')}
                <input
                  type="number"
                  min={5}
                  max={300}
                  disabled={!!remote?.automatic}
                  value={settings.interval}
                  onChange={(e) => setSettings({ ...settings, interval: Number(e.target.value) })}
                />
              </label>
              <label className="tool-checkbox">
                <input
                  type="checkbox"
                  checked={settings.sound}
                  disabled={!!remote?.automatic}
                  onChange={(e) => setSettings({ ...settings, sound: e.target.checked })}
                />
                {tr('Play notification sound')}
              </label>
            </div>
          </details>
          {(error || remote?.error) && (
            <p className="form-error" role="alert">
              {error || remote?.error}
            </p>
          )}
          {message && (
            <p className="fineprint" role="status">
              {message}
            </p>
          )}
          <div className="result-actions">
            <button
              className="standard-button"
              disabled={!!busy || !remote || !valid || remote.automatic}
              onClick={() => void act('send')}
            >
              <Send size={14} />
              {tr('Send once')}
            </button>
            {remote?.automatic ? (
              <button className="standard-button" disabled={!!busy} onClick={() => void act('stop')}>
                <Square size={14} />
                {tr('Stop automatic messages')}
              </button>
            ) : (
              <button
                className="standard-button"
                disabled={!!busy || !remote || !valid}
                onClick={() => void act('start')}
              >
                <Play size={14} />
                {tr('Start automatic messages')}
              </button>
            )}
            <button
              className="standard-button"
              disabled={!!busy || !remote || remote.automatic}
              onClick={() => void act('save')}
            >
              {busy && <LoaderCircle size={14} className="spin" />}
              {tr('Save settings')}
            </button>
          </div>
          <p className="fineprint">
            {tr(
              'Automatic messages continue in the tray. They start off when you reopen the app. Allow at least 5 seconds between messages.',
            )}
          </p>
        </section>
        <section className="dashboard-panel desktop-tool login-content" aria-labelledby="extract-title">
          <div className="desktop-tool-heading">
            <Archive size={20} />
            <div>
              <h3 id="extract-title">{tr('Unity package extractor')}</h3>
              <p>{tr('Get the original files without opening Unity.')}</p>
            </div>
          </div>
          <p className="fineprint">
            {tr(
              'Choose a .unitypackage and a destination folder. VRCove creates a new folder there and restores the asset names and folders.',
            )}
          </p>
          <label className="tool-checkbox">
            <input
              type="checkbox"
              checked={metadata}
              disabled={extracting}
              onChange={(e) => setMetadata(e.target.checked)}
            />
            {tr('Include Unity .meta files')}
          </label>
          <button className="standard-button" disabled={extracting} onClick={() => void extract()}>
            {extracting ? <LoaderCircle size={14} className="spin" /> : <FolderOpen size={14} />}
            {tr(extracting ? 'Choosing or extracting...' : 'Choose package and extract')}
          </button>
          {extractError && (
            <p className="form-error" role="alert">
              {extractError}
            </p>
          )}
          {extraction && (
            <div className="extraction-result" role="status">
              <strong>{tr('Extracted {count} files', { count: extraction.files })}</strong>
              <code>{extraction.folder}</code>
            </div>
          )}
          <p className="fineprint">
            {tr('Your files stay on this computer. Package scripts are never run.')}
          </p>
        </section>
      </div>
    </SectionPage>
  );
}
