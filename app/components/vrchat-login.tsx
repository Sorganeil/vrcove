import { useState } from 'react';
import { Compass, ArrowUpRight, ArrowRight, LoaderCircle, ShieldCheck, ExternalLink } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import Modal from './modal';
import type { VRChatUser } from '../lib/types';

// Connects a VRChat account, including two-factor verification.
export default function Login({
  close,
  connected,
  configured,
  expectedUserId,
  initialUsername,
}: {
  close: () => void;
  connected: (user: VRChatUser, similarName: boolean) => void;
  configured?: boolean;
  expectedUserId?: string;
  initialUsername?: string;
}) {
  useLocale();
  const [username, setUsername] = useState(initialUsername || ''),
    [password, setPassword] = useState(''),
    [code, setCode] = useState(''),
    [pending, setPending] = useState<string[] | null>(null),
    [type, setType] = useState('totp'),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const r = await api(
        pending ? '/account/2fa' : '/account/login',
        pending ? { code, type } : { username, password, ...(expectedUserId ? { expectedUserId } : {}) },
      );
      setPassword('');
      setCode('');
      if (r.requiresTwoFactorAuth) {
        setPending(r.requiresTwoFactorAuth);
        setType(r.requiresTwoFactorAuth.includes('totp') ? 'totp' : 'emailotp');
      } else connected(r.user, r.similarName);
    } catch (e) {
      setError((e as Error).message);
      setPassword('');
      setCode('');
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title={tr('Connect VRChat')} close={close}>
      <div className="login-content">
        <div className="login-icon">
          <Compass size={32} />
          <ArrowUpRight size={25} />
        </div>
        <div className="eyebrow">{tr('A PRIVATE CONNECTION')}</div>
        <h2>{pending ? tr('Two-factor verification') : tr('Connect to VRChat')}</h2>
        <p>
          {pending
            ? 'Enter your VRChat verification code.'
            : 'Link your VRChat account to your VRCove account to switch avatars, manage favorites, and find your friends.'}
        </p>
        <form onSubmit={submit}>
          {pending ? (
            <>
              <label>
                {tr('Verification method')}
                <select value={type} onChange={(e) => setType(e.target.value)}>
                  {pending.includes('totp') ? (
                    <>
                      <option value="totp">{tr('Authenticator app')}</option>
                      <option value="otp">{tr('Recovery code')}</option>
                    </>
                  ) : (
                    <option value="emailotp">{tr('Email code')}</option>
                  )}
                </select>
              </label>
              <label>
                {tr('Verification code')}
                <input
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  autoComplete="one-time-code"
                  autoFocus
                  required
                  maxLength={32}
                />
              </label>
            </>
          ) : (
            <>
              <label>
                {tr('VRChat username')}
                <input
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  required
                  maxLength={256}
                  placeholder={tr('Your username, not display name')}
                />
              </label>
              <label>
                {tr('Password')}
                <input
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  type="password"
                  autoComplete="current-password"
                  required
                  maxLength={1024}
                />
              </label>
            </>
          )}
          {error && (
            <div className="form-error" role="alert">
              {error}
            </div>
          )}
          {configured === false && (
            <div className="form-error">
              {tr('VRChat login is awaiting server configuration. Browsing and bookmarks are available.')}
            </div>
          )}
          <button className="primary" disabled={busy || configured === false} type="submit">
            {busy ? <LoaderCircle size={17} className="spin" /> : <ArrowRight size={17} />}{' '}
            {pending ? tr('Verify & connect') : tr('Connect to VRChat')}
          </button>
        </form>
        <div className="privacy-note">
          <ShieldCheck size={20} />
          <p>
            {tr(
              'Your password is processed briefly, then discarded. Your session stays in an encrypted browser cookie. No credentials saved on our server.',
            )}
          </p>
        </div>
        <a className="text-link" href="https://vrchat.com/home" target="_blank" rel="noreferrer">
          {tr('Need help with your account?')}
          <ExternalLink size={13} />
        </a>
      </div>
    </Modal>
  );
}
