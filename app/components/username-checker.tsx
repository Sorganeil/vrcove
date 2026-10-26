import { useEffect, useRef, useState } from 'react';
import { FileUp, LoaderCircle, Search, Square } from 'lucide-react';
import { api } from '../lib/api';
import { useApp } from '../lib/app-context';
import { t as tr } from '../lib/i18n';

type Row = { username: string; status: string };
type CheckState = {
  running: boolean;
  phase: string;
  rows: Row[];
  nextAt: number;
  waitReason: string;
  error: string;
};
const labels: Record<string, string> = {
  queued: 'Queued',
  checking: 'Checking',
  available: 'Available',
  taken: 'Taken',
  invalid: 'Invalid username',
  unconfirmed: 'Unconfirmed',
  error: 'Check failed',
};
const initial: CheckState = { running: false, phase: 'idle', rows: [], nextAt: 0, waitReason: '', error: '' };
function names(text: string) {
  return [
    ...new Set(
      text
        .split(/[\r\n,]+/)
        .map((name) => name.trim())
        .filter(Boolean),
    ),
  ];
}

export function UsernameChecker() {
  const { user, requestConnection } = useApp();
  const [text, setText] = useState('');
  const [state, setState] = useState(initial);
  const [error, setError] = useState('');
  const [pollError, setPollError] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [now, setNow] = useState(() => Date.now());
  const file = useRef<HTMLInputElement>(null);
  useEffect(() => {
    let alive = true;
    let timer: number;
    const refresh = async () => {
      try {
        const next: CheckState = await api('/desktop/tools/usernames');
        if (alive) {
          setState(next);
          setPollError('');
        }
      } catch (e) {
        if (alive) setPollError((e as Error).message);
      } finally {
        if (alive) {
          setLoading(false);
          timer = window.setTimeout(() => void refresh(), 1000);
        }
      }
    };
    void refresh();
    const clock = window.setInterval(() => setNow(Date.now()), 500);
    return () => {
      alive = false;
      clearTimeout(timer);
      clearInterval(clock);
    };
  }, []);
  const list = names(text);
  const count = state.rows.filter((r) => !['queued', 'checking'].includes(r.status)).length;
  const remaining = Math.max(0, Math.ceil((state.nextAt - now) / 1000));
  async function act(action: 'start' | 'stop') {
    setBusy(true);
    setError('');
    try {
      setState(
        await api('/desktop/tools/usernames', { action, ...(action === 'start' ? { usernames: list } : {}) }),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function loadFile(selected?: File) {
    if (!selected) return;
    setError('');
    if (selected.size > 65536) {
      setError('Choose a text file smaller than 64 KB.');
      return;
    }
    setBusy(true);
    try {
      const content = (await selected.text()).replace(/^\uFEFF/, '');
      if (content.includes('\0')) throw new Error('Choose a plain text file with one username per line.');
      setText(content);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="dashboard-panel desktop-tool login-content username-tool"
      aria-labelledby="username-check-title"
    >
      <div className="desktop-tool-heading">
        <Search size={20} />
        <div>
          <h3 id="username-check-title">{tr('Username checker')}</h3>
          <p>{tr('Check one username or a list of up to 100.')}</p>
        </div>
      </div>
      <p className="fineprint">
        {tr('Checks VRChat login usernames, not display names. Results do not reserve a name.')}
      </p>
      <div className="username-check-layout">
        <div className="username-check-input">
          <label htmlFor="username-list">{tr('Usernames')}</label>
          <textarea
            id="username-list"
            value={text}
            rows={5}
            maxLength={65536}
            disabled={state.running || busy}
            onChange={(e) => setText(e.target.value)}
            placeholder={tr('Enter one username, or paste a list separated by lines or commas.')}
          />
          <div className="username-input-actions">
            <input
              ref={file}
              type="file"
              hidden
              accept=".txt,text/plain"
              aria-label={tr('Username list file')}
              disabled={state.running || busy}
              onChange={(e) => {
                void loadFile(e.target.files?.[0]);
                e.target.value = '';
              }}
            />
            <button
              className="standard-button"
              disabled={state.running || busy}
              onClick={() => file.current?.click()}
            >
              <FileUp size={14} />
              {tr('Load .txt file')}
            </button>
            <small className={list.length > 100 ? 'danger-text' : 'fineprint'}>
              {tr('{count}/100 usernames', { count: list.length })}
            </small>
          </div>
          {list.length > 100 && (
            <p className="form-error" role="alert">
              {tr('Enter between 1 and 100 usernames.')}
            </p>
          )}
          <p className="fineprint">
            {tr(
              'One check every 2.5 seconds. Rate limits pause the queue automatically. Duplicate entries are checked once.',
            )}
          </p>
          {!user && (
            <button className="text-link" onClick={() => requestConnection()}>
              {tr('Sign in to VRChat')}
            </button>
          )}
          <div className="result-actions">
            {state.running ? (
              <button
                className="standard-button"
                disabled={busy || state.phase === 'stopping'}
                onClick={() => void act('stop')}
              >
                <Square size={14} />
                {tr(state.phase === 'stopping' ? 'Stopping...' : 'Stop checking')}
              </button>
            ) : (
              <button
                className="standard-button"
                disabled={!user || busy || loading || !list.length || list.length > 100}
                onClick={() => void act('start')}
              >
                {busy ? <LoaderCircle size={14} className="spin" /> : <Search size={14} />}
                {tr('Check usernames')}
              </button>
            )}
          </div>
          {(error || state.error || pollError) && (
            <p className="form-error" role="alert">
              {tr(error || state.error || pollError)}
            </p>
          )}
        </div>
        <div className="username-check-output">
          <p className="fineprint">
            {tr('Available is not a guarantee. Banned names or blacklisted words may still be unusable.')}
          </p>
          <div className="username-progress" role="status">
            {state.running && <LoaderCircle size={14} className="spin" />}
            <span>
              {tr(
                state.phase === 'complete'
                  ? 'Check complete'
                  : state.phase === 'stopped'
                    ? 'Check stopped'
                    : state.running
                      ? 'Checking usernames'
                      : 'Results appear here',
              )}
            </span>
            {state.rows.length > 0 && (
              <small>
                {count}/{state.rows.length}
              </small>
            )}
          </div>
          {state.running && remaining > 0 && state.waitReason && (
            <p className="fineprint username-wait">
              {tr(
                state.waitReason === 'rate-limit'
                  ? 'VRChat rate limit. Retrying in {seconds}s.'
                  : 'Connection problem. Retrying in {seconds}s.',
                { seconds: remaining },
              )}
            </p>
          )}
          {state.rows.length > 0 && (
            <progress max={state.rows.length} value={count} aria-label={tr('Username check progress')} />
          )}
          <ul className="username-results" aria-label={tr('Username results')}>
            {state.rows.map((row) => (
              <li key={row.username}>
                <span>{row.username}</span>
                <strong className={'username-result-' + row.status}>
                  {tr(labels[row.status] || 'Check failed')}
                </strong>
              </li>
            ))}
          </ul>
          {state.rows.some((row) => row.status === 'unconfirmed') && (
            <p className="fineprint">
              {tr('Unconfirmed means the name was not found, but VRChat did not confirm it can be used.')}
            </p>
          )}
        </div>
      </div>
    </section>
  );
}
