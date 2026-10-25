import { useEffect, useState } from 'react';
import { KeyRound, Pause, Play, Trash2, MonitorDown } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';

type LoggerState = {
  status: string;
  paused: boolean;
  detected: number;
  added: number;
  existing: number;
  rejected: number;
  pending: number;
  events: { time: string; id: string; result: string }[];
  folder: string;
  autoStart: boolean;
  hasKey: boolean;
};

/** The avatar logger built into the desktop app, in place of the companion download page. */
export default function DesktopLogger() {
  useLocale();
  const { user, requestConnection, notify } = useApp();
  const [state, setState] = useState<LoggerState | null>(null);
  const [key, setKey] = useState('');
  const [folder, setFolder] = useState<string | null>(null);
  const send = (body: object) =>
    api('/desktop/logger', body)
      .then(setState)
      .catch((e) => notify(e.message));
  useEffect(() => {
    const load = () =>
      api('/desktop/logger')
        .then(setState)
        .catch(() => {});
    void load();
    const timer = window.setInterval(load, 3000);
    return () => window.clearInterval(timer);
  }, []);
  if (!state) return null;
  const counts: [string, number][] = [
    ['Detected', state.detected],
    ['Added', state.added],
    ['Already known', state.existing],
    ['Waiting', state.pending],
    ['Skipped', state.rejected],
  ];
  return (
    <div className="settings-page desktop-logger">
      <div className="page-title">
        <div>
          <h1>{tr('Avatar logger')}</h1>
          <p>{tr('Adds public avatars you see in VRChat to VRCove’s search while you play.')}</p>
        </div>
        <MonitorDown size={25} />
      </div>
      <section className="dashboard-panel">
        <div className="panel-title">
          <h2>{tr(state.status || 'Paused')}</h2>
          <button
            className="standard-button"
            onClick={() => void send({ action: state.paused ? 'start' : 'pause' })}
          >
            {state.paused ? <Play size={14} /> : <Pause size={14} />}
            {tr(state.paused ? 'Start' : 'Pause')}
          </button>
        </div>
        <div className="settings-content">
          <dl className="logger-counts">
            {counts.map(([label, n]) => (
              <div key={label}>
                <dt>{tr(label)}</dt>
                <dd>{n}</dd>
              </div>
            ))}
          </dl>
          {state.events.length > 0 && (
            <ul className="logger-events">
              {state.events.map((e) => (
                <li key={e.time + e.id}>
                  <code>{e.id}</code> <span>{e.result}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
      <div className="settings-grid">
        <section className="dashboard-panel">
          <div className="panel-title">
            <h2>{tr('Uploading')}</h2>
            <KeyRound size={16} />
          </div>
          <div className="settings-content">
            <p>
              {tr(
                'Avatars are checked with your VRChat sign-in on this device, then sent with a VRCove API key. Generate one on your VRCove account page after linking VRChat there; the website checks every avatar again.',
              )}
            </p>
            {!user && (
              <p>
                <button className="text-link" onClick={() => requestConnection()}>
                  {tr('Sign in to VRChat')}
                </button>
              </p>
            )}
            <form
              className="logger-key"
              onSubmit={(e) => {
                e.preventDefault();
                void send({ token: key }).then(() => setKey(''));
              }}
            >
              <label>
                {tr(state.hasKey ? 'Replace API key' : 'VRCove API key')}
                <input
                  type="password"
                  autoComplete="off"
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                  placeholder={state.hasKey ? tr('Saved') : ''}
                />
              </label>
              <button className="standard-button" type="submit" disabled={!key.trim()}>
                {tr('Save key')}
              </button>
            </form>
          </div>
        </section>
        <section className="dashboard-panel">
          <div className="panel-title">
            <h2>{tr('Collection')}</h2>
          </div>
          <div className="settings-content">
            <form
              className="logger-key"
              onSubmit={(e) => {
                e.preventDefault();
                if (folder !== null) void send({ folder }).then(() => setFolder(null));
              }}
            >
              <label>
                {tr('VRChat analytics folder')}
                <input value={folder ?? state.folder} onChange={(e) => setFolder(e.target.value)} />
              </label>
              <button className="standard-button" type="submit" disabled={folder === null}>
                {tr('Save folder')}
              </button>
            </form>
            <label className="cove-switch">
              <input
                type="checkbox"
                checked={state.autoStart}
                onChange={(e) => void send({ autoStart: e.target.checked })}
              />
              <span>{tr('Start collecting when the app opens')}</span>
            </label>
            <div className="settings-actions">
              <button className="standard-button" onClick={() => void send({ action: 'clear-cache' })}>
                <Trash2 size={14} />
                {tr('Clear analytics cache')}
              </button>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
