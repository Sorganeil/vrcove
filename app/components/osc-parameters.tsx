import { useEffect, useMemo, useState } from 'react';
import { LoaderCircle, Power, PowerOff, RefreshCw, SlidersHorizontal } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';

type Parameter = {
  name: string;
  type: 'Bool' | 'Int' | 'Float';
  writable: boolean;
  value?: boolean | number;
};
type OscStatus = {
  listening: boolean;
  port: number;
  defaultPort: number;
  afk: boolean;
  avatarId: string;
  avatarName: string;
  parameters: Parameter[];
};

const pollMilliseconds = 1000;

function ParameterControl({
  parameter,
  send,
}: {
  parameter: Parameter;
  send: (name: string, value: boolean | number) => Promise<void>;
}) {
  // The control shows the player's change until VRChat's value catches up.
  // Sliders send on release, so dragging doesn't flood VRChat.
  const [draft, setDraft] = useState<boolean | number | null>(null);
  const { name, type, value } = parameter;
  const shown = draft ?? value;
  if (!parameter.writable) return <output>{value === undefined ? tr('Not seen yet') : String(value)}</output>;
  const commit = (next: boolean | number | null = draft) => {
    if (next === null) return;
    void send(name, next).finally(() => setDraft(null));
  };
  if (type === 'Bool')
    return (
      <input
        type="checkbox"
        aria-label={name}
        checked={shown === true}
        onChange={(e) => {
          setDraft(e.target.checked);
          commit(e.target.checked);
        }}
      />
    );
  const number = typeof shown === 'number' ? shown : 0;
  return type === 'Int' ? (
    <input
      type="number"
      aria-label={name}
      min={0}
      max={255}
      value={number}
      onChange={(e) => setDraft(Math.min(255, Math.max(0, Math.trunc(Number(e.target.value)))))}
      onBlur={() => commit()}
      onKeyDown={(e) => e.key === 'Enter' && commit()}
    />
  ) : (
    <span className="osc-slider">
      <input
        type="range"
        aria-label={name}
        min={-1}
        max={1}
        step={0.01}
        value={number}
        onChange={(e) => setDraft(Number(e.target.value))}
        onPointerUp={() => commit()}
        onKeyUp={() => commit()}
        onBlur={() => commit()}
      />
      <output>{number.toFixed(2)}</output>
    </span>
  );
}

/* Controls the parameters of the avatar the player is wearing. VRChat lists
   them in a config file per avatar, and reports values over OSC. */
export function OscParameters() {
  useLocale();
  const [status, setStatus] = useState<OscStatus | null>(null);
  const [port, setPort] = useState(9001);
  const [search, setSearch] = useState('');
  const [readOnly, setReadOnly] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    let alive = true;
    api('/desktop/tools/osc')
      .then((s: OscStatus) => {
        if (!alive) return;
        setStatus(s);
        setPort(s.port || s.defaultPort);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, []);
  const listening = !!status?.listening;
  useEffect(() => {
    if (!listening) return;
    const timer = window.setInterval(() => {
      api('/desktop/tools/osc')
        .then(setStatus)
        .catch(() => {});
    }, pollMilliseconds);
    return () => clearInterval(timer);
  }, [listening]);
  const shown = useMemo(() => {
    const term = search.trim().toLowerCase();
    return (status?.parameters ?? [])
      .filter((p) => (readOnly || p.writable) && p.name.toLowerCase().includes(term))
      .sort((a, b) => Number(b.writable) - Number(a.writable) || a.name.localeCompare(b.name));
  }, [status?.parameters, search, readOnly]);
  async function act(action: string, body: object = {}) {
    setBusy(action);
    setError('');
    try {
      setStatus(await api('/desktop/tools/osc', { action, ...body }));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  const send = (name: string, value: boolean | number) => {
    setError('');
    return api('/desktop/tools/osc', { action: 'set', name, value })
      .then(setStatus)
      .catch((e) => setError(e.message));
  };
  return (
    <section className="dashboard-panel desktop-tool login-content osc-tool" aria-labelledby="osc-title">
      <div className="desktop-tool-heading">
        <SlidersHorizontal size={20} />
        <div>
          <h3 id="osc-title">{tr('Avatar parameters')}</h3>
          <p>{tr('Toggle and adjust the avatar you are wearing.')}</p>
        </div>
        <span className={'tool-state' + (listening ? ' active' : '')}>
          {tr(listening ? 'Connected' : 'Not connected')}
        </span>
      </div>
      <p className="fineprint">
        {tr(
          'VRCove listens for VRChat on this PC while connected. Only one app can use a port, so turn this off if another OSC app needs it.',
        )}
      </p>
      <div className="osc-connect">
        <label>
          {tr('Listen port')}
          <input
            type="number"
            min={1024}
            max={65535}
            value={port}
            disabled={listening}
            onChange={(e) => setPort(Number(e.target.value))}
          />
        </label>
        {listening ? (
          <button className="standard-button" disabled={!!busy} onClick={() => void act('stop')}>
            <PowerOff size={14} />
            {tr('Disconnect')}
          </button>
        ) : (
          <button
            className="standard-button"
            disabled={!!busy || !status}
            onClick={() => void act('listen', { port })}
          >
            {busy === 'listen' ? <LoaderCircle size={14} className="spin" /> : <Power size={14} />}
            {tr('Connect to VRChat')}
          </button>
        )}
        {listening && (
          <button className="standard-button" disabled={!!busy} onClick={() => void act('reload')}>
            <RefreshCw size={14} />
            {tr('Read avatar again')}
          </button>
        )}
      </div>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {listening && !status?.avatarId && (
        <p className="fineprint">
          {tr(
            'No avatar found yet. Switch avatars in VRChat with OSC enabled, and VRChat will list its parameters.',
          )}
        </p>
      )}
      {listening && !!status?.avatarId && (
        <>
          <p className="osc-avatar">
            <strong>{status.avatarName || status.avatarId}</strong>
            {status.afk && <span className="tool-state active">{tr('AFK')}</span>}
          </p>
          <div className="osc-filter">
            <input
              type="search"
              aria-label={tr('Search parameters')}
              placeholder={tr('Search parameters')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <label className="tool-checkbox">
              <input type="checkbox" checked={readOnly} onChange={(e) => setReadOnly(e.target.checked)} />
              {tr('Show read-only parameters')}
            </label>
          </div>
          {shown.length === 0 ? (
            <p className="fineprint">{tr('No parameters match.')}</p>
          ) : (
            <ul className="osc-parameters">
              {shown.map((parameter) => (
                <li key={parameter.name}>
                  <span>
                    {parameter.name}
                    <small>{parameter.type}</small>
                  </span>
                  <ParameterControl parameter={parameter} send={send} />
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}
