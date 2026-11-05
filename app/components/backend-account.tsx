import { useState, type FormEvent } from 'react';
import { KeyRound, LoaderCircle, LogOut, RefreshCw, ShieldCheck } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useResource } from '../lib/use-resource';

type BackendStatus = {
  state: 'active' | 'code' | 'signed-out' | 'needs-sign-in';
  username: string;
  displayName: string;
  userId: string;
  problem: '' | 'code' | 'failed';
  passwordSaved: boolean;
  canRetry: boolean;
  loggerEnabled: boolean;
  methods: string[];
  signedIn: number;
  nextSignIn: number;
  rateLimitedFor: number;
};

const when = (seconds: number) => new Date(seconds * 1000).toLocaleString(document.documentElement.lang);
const methodLabels: Record<string, string> = {
  emailOtp: 'Email code',
  totp: 'Authenticator app',
  otp: 'Recovery code',
};

/* Owners sign in VRCove's own VRChat account here. The server keeps the
   password and session sealed, and they never come back to the browser. */
export function BackendAccount({ notify }: { notify: (s: string) => void }) {
  useLocale();
  const resource = useResource('/cove/admin/bot');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [method, setMethod] = useState('');
  const [code, setCode] = useState('');
  const [replacing, setReplacing] = useState(false);
  const status: BackendStatus | null = resource.data;
  if (!status) return <p className="fineprint">{resource.error || tr('Loading…')}</p>;

  async function act(action: string, body: object = {}, message = '') {
    setBusy(action);
    setError('');
    try {
      const next: BackendStatus = await api('/cove/admin/bot', { action, ...body });
      setPassword('');
      setCode('');
      if (next.state === 'active' && action !== 'check') setReplacing(false);
      if (message) notify(message);
      resource.reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  const verify = (e: FormEvent) => {
    e.preventDefault();
    void act('verify', { method: method || status.methods[0], code }, tr('Backend account signed in'));
  };
  const signIn = (e: FormEvent) => {
    e.preventDefault();
    void act('login', { username, password });
  };
  const active = status.state === 'active';
  const showForm = status.state !== 'code' && (!active || replacing);
  return (
    <div className="admin-list backend-account">
      <article className="admin-card">
        <header>
          <KeyRound size={14} />
          <strong>{status.displayName || status.username || tr('VRChat account')}</strong>
          <span className={'admin-pill' + (active ? ' good' : ' bad')}>
            {tr(
              active
                ? 'Signed in'
                : status.state === 'code'
                  ? 'Waiting for a code'
                  : status.state === 'signed-out'
                    ? 'Signed out'
                    : 'Needs sign-in',
            )}
          </span>
        </header>
        <p className="fineprint">
          {tr(
            status.loggerEnabled
              ? 'VRCove uses this VRChat account to check avatars sent by the desktop logger.'
              : 'VRCove uses this VRChat account to check logger submissions. The logger is off on this server, so nothing uses it yet.',
          )}
        </p>
        <dl className="backend-details">
          {status.username && (
            <>
              <dt>{tr('Username')}</dt>
              <dd>{status.username}</dd>
            </>
          )}
          {status.userId && (
            <>
              <dt>{tr('VRChat user ID')}</dt>
              <dd>
                <code>{status.userId}</code>
              </dd>
            </>
          )}
          {status.signedIn > 0 && (
            <>
              <dt>{tr('Signed in')}</dt>
              <dd>{when(status.signedIn)}</dd>
            </>
          )}
          <dt>{tr('Automatic sign-in')}</dt>
          <dd>
            {tr(
              status.passwordSaved
                ? 'Uses the password saved here'
                : status.canRetry
                  ? 'Uses the password in the server environment'
                  : 'Off until you sign in',
            )}
          </dd>
        </dl>
        {status.problem === 'code' && (
          <p className="admin-note">
            {tr('VRChat asked for a new code, so avatar checks are paused. Sign in to enter it.')}
          </p>
        )}
        {status.problem === 'failed' && (
          <p className="admin-note">
            {tr('The last automatic sign-in failed, so avatar checks are paused. Sign in to try again.')}
          </p>
        )}
        {status.nextSignIn > 0 && !status.problem && (
          <p className="fineprint">{tr('Next automatic try: {time}', { time: when(status.nextSignIn) })}</p>
        )}
        {status.rateLimitedFor > 0 && (
          <p className="admin-note">
            {tr('VRChat is rate limiting this account for {seconds} more seconds.', {
              seconds: status.rateLimitedFor,
            })}
          </p>
        )}
        {status.state === 'code' && (
          <form className="backend-form" onSubmit={verify}>
            <p>{tr('Enter the code VRChat sent for {name}.', { name: status.username })}</p>
            {status.methods.length > 1 && (
              <label>
                {tr('Code type')}
                <select value={method || status.methods[0]} onChange={(e) => setMethod(e.target.value)}>
                  {status.methods.map((m) => (
                    <option key={m} value={m}>
                      {tr(methodLabels[m] || m)}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <label>
              {tr('Code')}
              <input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoComplete="one-time-code"
                inputMode="text"
                maxLength={16}
                autoFocus
              />
            </label>
            <div className="result-actions">
              <button className="standard-button" disabled={!!busy || code.trim().length < 6}>
                {busy === 'verify' ? <LoaderCircle size={13} className="spin" /> : <ShieldCheck size={13} />}
                {tr('Verify code')}
              </button>
              <button
                type="button"
                className="standard-button"
                disabled={!!busy}
                onClick={() => void act('cancel')}
              >
                {tr('Cancel')}
              </button>
            </div>
          </form>
        )}
        {showForm && status.canRetry && !replacing && (
          <div className="result-actions">
            <button className="standard-button" disabled={!!busy} onClick={() => void act('login')}>
              {busy === 'login' ? <LoaderCircle size={13} className="spin" /> : <RefreshCw size={13} />}
              {tr('Sign in again')}
            </button>
          </div>
        )}
        {showForm && (
          <form className="backend-form" onSubmit={signIn}>
            <p className="fineprint">
              {tr(
                'VRCove encrypts the password on the server so it can sign in again when the session ends. It never shows it here.',
              )}
            </p>
            <label>
              {tr('VRChat username or email')}
              <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="off"
                maxLength={256}
              />
            </label>
            <label>
              {tr('Password')}
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                maxLength={1024}
              />
            </label>
            <div className="result-actions">
              <button className="standard-button" disabled={!!busy || !username.trim() || !password}>
                {busy === 'login' ? <LoaderCircle size={13} className="spin" /> : <KeyRound size={13} />}
                {tr('Sign in')}
              </button>
              {replacing && (
                <button
                  type="button"
                  className="standard-button"
                  disabled={!!busy}
                  onClick={() => setReplacing(false)}
                >
                  {tr('Cancel')}
                </button>
              )}
            </div>
          </form>
        )}
        {active && !replacing && (
          <div className="result-actions">
            <button
              className="standard-button"
              disabled={!!busy}
              onClick={() => void act('check', {}, tr('Session checked'))}
            >
              {busy === 'check' ? <LoaderCircle size={13} className="spin" /> : <RefreshCw size={13} />}
              {tr('Check session')}
            </button>
            <button className="standard-button" disabled={!!busy} onClick={() => setReplacing(true)}>
              <KeyRound size={13} />
              {tr('Use another account')}
            </button>
            <button
              className="standard-button danger-text"
              disabled={!!busy}
              onClick={() =>
                confirm(
                  tr(
                    'Signing out forgets the saved password and pauses avatar checks until you sign in again.',
                  ),
                ) && void act('logout', {}, tr('Backend account signed out'))
              }
            >
              <LogOut size={13} />
              {tr('Sign out')}
            </button>
          </div>
        )}
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
      </article>
    </div>
  );
}
