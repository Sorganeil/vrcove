import { api, useVRChatSession, useSessionTime, vrchatAccountState } from '../lib/api';
import { formatDate, t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect } from 'react';
import Turnstile from '../components/turnstile';
import {
  ShieldCheck,
  LoaderCircle,
  Link2,
  Unlink,
  LogOut,
  KeyRound,
  Trash2,
  Download,
  UserRound,
  TriangleAlert,
  ArrowRight,
  Check,
  EyeOff,
  Globe2,
  Lock,
  Library,
  Upload,
  Monitor,
  Smartphone,
  Tablet,
  X,
  Pencil,
} from 'lucide-react';
import { covePath, appLink } from '../lib/routes';
import { CovePicture } from '../components/cove-picture';
import PictureCropper from '../components/picture-cropper';
import LoggerApiKeys from '../components/logger-api-keys';
import { day } from '../lib/dates';
import type { VRChatUser } from '../lib/types';
type Me = {
  recoveryCodesRemaining: number;
  picture: string;
  username: string;
  created: string;
  bio: string;
  status: string;
  public: boolean;
  links: {
    id: number;
    vrchatId: string;
    displayName: string;
    username?: string;
    linked: string;
    active?: boolean;
    sessionExpires?: number;
    sessions?: VRChatSession[];
  }[];
};
type VRChatSession = {
  id: number;
  device: 'phone' | 'tablet' | 'computer';
  browser: string;
  started: string;
  expires: number;
  current: boolean;
};
const deviceIcons = { phone: Smartphone, tablet: Tablet, computer: Monitor };
const deviceNames = { phone: 'Phone', tablet: 'Tablet', computer: 'Computer' };
/* Mirrors similarNames in internal/server/accounts.go. */
const fold = (s: string) => s.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '');
export function similarNames(cove: string, vrchat: string) {
  let a = fold(cove),
    b = fold(vrchat);
  if (!a || !b) return false;
  if ([...a].length > [...b].length) [a, b] = [b, a];
  return a === b || ([...a].length >= 4 && b.includes(a));
}

function NameAdvice() {
  useLocale();
  return (
    <div className="privacy-note">
      <EyeOff size={19} />
      <p>
        <strong>{tr('Pick a name unrelated to your VRChat name.')}</strong>{' '}
        {tr(
          'Your VRCove username appears on every review you post. If it matches or resembles your VRChat display name or username, anyone, including VRChat staff, could connect your activity here to your VRChat account. Avoid reusing a handle you use elsewhere, too. A different name reduces correlation risk; it cannot guarantee anonymity or prevent action by VRChat.',
        )}
      </p>
    </div>
  );
}

/* #account: sign in, sign up, private VRChat links, password and deletion. */
export default function AccountPage({
  user,
  configured,
  onLogin,
  notify,
  onAccountChange,
  onSwitch,
  onDisconnected,
}: {
  user: VRChatUser | null;
  configured?: boolean;
  onLogin: (id?: string, username?: string) => void;
  notify: (s: string) => void;
  onAccountChange: (me: Me | null) => void;
  onSwitch: (id: number) => Promise<void>;
  onDisconnected?: () => void;
}) {
  useLocale();
  const connection = useVRChatSession();
  const now = useSessionTime();
  const [me, setMe] = useState<Me | null>(null),
    [cropping, setCropping] = useState<File | null>(null),
    [loading, setLoading] = useState(true),
    [mode, setMode] = useState<'login' | 'signup' | 'recover'>('login');
  const [username, setUsername] = useState(''),
    [password, setPassword] = useState(''),
    [confirm, setConfirm] = useState(''),
    [busy, setBusy] = useState(''),
    [error, setError] = useState(''),
    [warning, setWarning] = useState('');
  const [siteKey, setSiteKey] = useState(''),
    [challenge, setChallenge] = useState(''),
    [attempt, setAttempt] = useState(0);
  useEffect(() => {
    api('/stats')
      .then((s) => setSiteKey(typeof s.turnstileSiteKey === 'string' ? s.turnstileSiteKey : ''))
      .catch(() => {});
  }, []);
  const [recoveryCode, setRecoveryCode] = useState(''),
    [recoveryPassword, setRecoveryPassword] = useState(''),
    [codes, setCodes] = useState<string[]>([]);
  const [bio, setBio] = useState<string | null>(null),
    [status, setStatus] = useState<string | null>(null);
  const [current, setCurrent] = useState(''),
    [next, setNext] = useState(''),
    [deletePassword, setDeletePassword] = useState(''),
    [deleting, setDeleting] = useState(false),
    [renaming, setRenaming] = useState(false),
    [newName, setNewName] = useState(''),
    [renamePassword, setRenamePassword] = useState(''),
    [exportPassword, setExportPassword] = useState(''),
    [exporting, setExporting] = useState(false);
  function updateMe(value: Me | null) {
    if (!value) setCodes([]);
    setMe(value);
    onAccountChange(value);
  }
  const load = () =>
    api('/cove/me')
      .then(updateMe)
      .catch((e) => {
        if (e.status === 401) updateMe(null);
        else setError(e.message);
      })
      .finally(() => setLoading(false));
  // Refresh private links after the VRChat linking dialog finishes.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    load();
  }, [user?.id]);
  async function run(name: string, fn: () => Promise<void>) {
    setBusy(name);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (mode !== 'login' && password !== confirm) {
      setError(tr('The passwords don’t match.'));
      return;
    }
    if (mode === 'signup' && siteKey && !challenge) {
      setError(tr('Complete the verification check and try again.'));
      return;
    }
    run('auth', async () => {
      if (mode === 'recover') {
        await api('/cove/recover', { username, code: recoveryCode, next: password });
        setRecoveryCode('');
        setPassword('');
        setConfirm('');
        setMode('login');
        notify(tr('Password reset. Sign in with your new password.'));
        return;
      }
      const signup = mode === 'signup';
      if (signup) setAttempt((a) => a + 1);
      const r = await api(
        '/cove/' + mode,
        signup ? { username, password, turnstile: challenge } : { username, password },
      );
      setPassword('');
      setConfirm('');
      updateMe(r);
      notify(mode === 'signup' ? 'VRCove account created' : 'Signed in as ' + r.username);
    });
  }
  function generateCodes(e: React.FormEvent) {
    e.preventDefault();
    run('recovery', async () => {
      const result = await api('/cove/recovery-codes', { password: recoveryPassword });
      setCodes(result.codes);
      setRecoveryPassword('');
      await load();
    });
  }
  function downloadCodes() {
    const url = URL.createObjectURL(
      new Blob(
        [
          'VRCove recovery codes for ' +
            me!.username +
            '\nEach code can be used once. Keep these private.\n\n' +
            codes.join('\n'),
        ],
        { type: 'text/plain' },
      ),
    );
    const link = document.createElement('a');
    link.href = url;
    link.download = 'vrcove-recovery-codes.txt';
    link.click();
    URL.revokeObjectURL(url);
  }

  // Ending this browser's session disconnects VRChat here; ending another device's locks it out.
  const endSession = (s: VRChatSession) =>
    run('end:' + s.id, async () => {
      const r = await api('/cove/vrchat-sessions/end', { id: s.id });
      if (s.current) onDisconnected?.();
      updateMe(r);
      notify(tr(s.current ? 'Disconnected from VRChat' : 'Session ended'));
    });
  const unlink = (id: number) =>
    run('unlink:' + id, async () => {
      await api('/cove/unlink', { id });
      await load();
      notify(tr('VRChat account unlinked'));
    });
  const switchAccount = (id: number) =>
    run('switch:' + id, async () => {
      try {
        await onSwitch(id);
        await load();
        notify(tr('Active VRChat account changed'));
      } catch (e) {
        await load();
        throw e;
      }
    });
  const logout = () =>
    run('logout', async () => {
      await api('/cove/logout', {});
      updateMe(null);
      setMode('login');
      setWarning('');
      notify(tr('Signed out of VRCove'));
    });
  const saveProfile = (patch: { public?: boolean; bio?: string; status?: string }) =>
    run('profile', async () => {
      const previous = me;
      if (me) setMe({ ...me, ...patch });
      try {
        updateMe(await api('/cove/profile', patch));
      } catch (e) {
        setMe(previous);
        throw e;
      }
      if (patch.bio !== undefined) setBio(null);
      if (patch.status !== undefined) setStatus(null);
      notify(
        patch.public === undefined
          ? 'VRCove profile saved'
          : patch.public
            ? 'Your profile is now public'
            : 'Your profile is now private',
      );
    });
  async function uploadPicture(picture: Blob) {
    if (picture.size > 2 * 1024 * 1024) throw new Error(tr('Choose an image smaller than 2 MB.'));
    const response = await fetch('/api/cove/picture', { method: 'POST', body: picture });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'Upload failed');
    updateMe(result);
    setCropping(null);
    notify(tr('Profile picture updated'));
  }
  const removePicture = () =>
    run('picture', async () => {
      updateMe(await api('/cove/picture/remove', {}));
      notify(tr('Profile picture removed'));
    });
  function changePassword(e: React.FormEvent) {
    e.preventDefault();
    run('password', async () => {
      await api('/cove/password', { current, next });
      setCurrent('');
      setNext('');
      notify(tr('Password changed. Other devices were signed out.'));
    });
  }
  function rename(e: React.FormEvent) {
    e.preventDefault();
    run('rename', async () => {
      updateMe(await api('/cove/username', { username: newName.trim(), password: renamePassword }));
      setNewName('');
      setRenamePassword('');
      setRenaming(false);
      notify(tr('Username changed'));
    });
  }
  function downloadData(e: React.FormEvent) {
    e.preventDefault();
    run('export', async () => {
      const response = await fetch('/api/cove/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: exportPassword }),
      });
      if (!response.ok) throw new Error((await response.json()).error || 'Download failed');
      const name = /filename="([^"]+)"/.exec(response.headers.get('Content-Disposition') || '')?.[1];
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement('a');
      link.href = url;
      link.download = name || 'vrcove-data.json';
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      setExportPassword('');
      setExporting(false);
      notify(tr('Your data was downloaded'));
    });
  }
  function remove(e: React.FormEvent) {
    e.preventDefault();
    run('delete', async () => {
      await api('/cove/delete', { password: deletePassword });
      setDeletePassword('');
      setDeleting(false);
      updateMe(null);
      notify(tr('Your VRCove account, reviews and collections were deleted'));
    });
  }
  const heading = (
    <div className="section-heading">
      <div>
        <h2>{tr('VRCove account')}</h2>
        <p>
          {tr('Create or sign in to your VRCove account, then link VRChat to use authenticated actions.')}
        </p>
      </div>
    </div>
  );
  if (configured === false)
    return (
      <section className="catalog-section">
        {heading}
        <div className="empty-state">
          <ShieldCheck size={30} />
          <h3>{tr('Accounts aren’t enabled here')}</h3>
          <p>{tr('This deployment has no ACCOUNT_KEY configured.')}</p>
        </div>
      </section>
    );
  if (loading)
    return (
      <section className="catalog-section">
        {heading}
        <div className="loading-state">
          <LoaderCircle className="spin" />
          {tr('Loading…')}
        </div>
      </section>
    );
  if (!me) {
    const similar =
      mode === 'signup' &&
      user?.displayName &&
      username.length >= 3 &&
      similarNames(username, user.displayName);
    return (
      <section className="catalog-section">
        {heading}
        <div className="cove-auth">
          <div className="login-content">
            <div className="result-actions">
              <button
                className={mode === 'login' ? 'chosen' : ''}
                disabled={!!busy}
                onClick={() => {
                  setMode('login');
                  setPassword('');
                  setConfirm('');
                  setError('');
                }}
              >
                {tr('Sign in')}
              </button>
              <button
                className={mode === 'signup' ? 'chosen' : ''}
                disabled={!!busy}
                onClick={() => {
                  setMode('signup');
                  setPassword('');
                  setConfirm('');
                  setError('');
                }}
              >
                {tr('Create account')}
              </button>
            </div>
            {mode === 'recover' && (
              <p className="fineprint">
                {tr('Reset your password with a saved recovery code. Each code can be used once.')}
              </p>
            )}
            <form onSubmit={submit}>
              <label>
                {tr('VRCove username')}
                <input
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  required
                  minLength={3}
                  maxLength={32}
                  pattern="[A-Za-z0-9_.\-]+"
                  title={tr('Letters, numbers, dots, dashes or underscores')}
                />
              </label>
              {similar && (
                <div className="form-error" role="alert">
                  <TriangleAlert size={14} /> {tr('This looks like your VRChat name,')} {user.displayName}
                  {tr('. Choose something unrelated.')}
                </div>
              )}
              {mode === 'recover' && (
                <label>
                  {tr('Recovery code')}
                  <input
                    value={recoveryCode}
                    onChange={(e) => setRecoveryCode(e.target.value)}
                    autoComplete="off"
                    spellCheck={false}
                    autoCapitalize="none"
                    required
                    maxLength={128}
                  />
                </label>
              )}
              <label>
                {tr(mode === 'recover' ? 'New password' : 'Password')}
                <input
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  type="password"
                  autoComplete={mode !== 'login' ? 'new-password' : 'current-password'}
                  required
                  minLength={mode !== 'login' ? 10 : 1}
                  maxLength={1024}
                />
              </label>
              {mode !== 'login' && (
                <label>
                  {tr('Confirm password')}
                  <input
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    type="password"
                    autoComplete="new-password"
                    required
                    maxLength={1024}
                  />
                </label>
              )}
              {mode === 'signup' && siteKey && (
                <>
                  <Turnstile siteKey={siteKey} onToken={setChallenge} resetKey={attempt} />
                  <p className="fineprint">
                    {tr('Cloudflare Turnstile checks for bots. It loads only on this form.')}
                  </p>
                </>
              )}
              {error && (
                <div className="form-error" role="alert">
                  {error}
                </div>
              )}
              <button className="primary" type="submit" disabled={!!busy}>
                {busy ? <LoaderCircle size={16} className="spin" /> : <ArrowRight size={16} />}
                {mode === 'signup'
                  ? tr('Create account')
                  : mode === 'recover'
                    ? tr('Reset password')
                    : tr('Sign in')}
              </button>
            </form>
            {mode === 'login' && (
              <button
                className="standard-button"
                disabled={!!busy}
                onClick={() => {
                  setMode('recover');
                  setPassword('');
                  setConfirm('');
                  setError('');
                }}
              >
                {tr('Forgot password? Use a recovery code')}
              </button>
            )}
            {mode === 'signup' && (
              <>
                <NameAdvice />
                <p className="fineprint">
                  {tr(
                    'No email is collected. After creating your account, generate recovery codes below and keep them in a safe place. Without your password or a saved recovery code, your account cannot be recovered. Passwords are stored as Argon2id hashes.',
                  )}
                </p>
              </>
            )}
          </div>
        </div>
      </section>
    );
  }
  return (
    <section className="catalog-section">
      {heading}
      {warning && (
        <div className="form-error cove-warning" role="alert">
          <TriangleAlert size={15} />
          {warning}
          <button onClick={() => setWarning('')}>{tr('Dismiss')}</button>
        </div>
      )}
      <div className="cove-panels">
        <div className="cove-panel">
          <div className="eyebrow">{tr('SIGNED IN')}</div>
          <h3>
            <CovePicture picture={me.picture} username={me.username} size={40} />
            {me.username}
          </h3>
          <p className="fineprint">
            {tr('Member since')} {day(me.created)}
            {tr('. Your username is public on your reviews.')}
          </p>
          {renaming ? (
            <form className="login-content" onSubmit={rename}>
              <label>
                {tr('New username')}
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  autoComplete="username"
                  required
                  minLength={3}
                  maxLength={32}
                  pattern="[A-Za-z0-9_.\-]{3,32}"
                />
              </label>
              {newName.trim().length >= 3 &&
                me.links.some(
                  (l) =>
                    similarNames(newName, l.displayName) ||
                    (!!l.username && similarNames(newName, l.username)),
                ) && <NameAdvice />}
              <label>
                {tr('Confirm with your password')}
                <input
                  type="password"
                  value={renamePassword}
                  onChange={(e) => setRenamePassword(e.target.value)}
                  autoComplete="current-password"
                  required
                  maxLength={1024}
                />
              </label>
              <p className="fineprint">
                {tr(
                  'Your reviews and collections keep working under the new name. Old links to your profile stop working, and anyone can take your old name.',
                )}
              </p>
              <div className="result-actions">
                <button className="standard-button" type="submit" disabled={!!busy}>
                  {busy === 'rename' ? <LoaderCircle size={13} className="spin" /> : <Check size={13} />}
                  {tr('Change username')}
                </button>
                <button type="button" className="standard-button" onClick={() => setRenaming(false)}>
                  {tr('Cancel')}
                </button>
              </div>
            </form>
          ) : (
            <div className="result-actions">
              <button className="standard-button" disabled={!!busy} onClick={() => setRenaming(true)}>
                <Pencil size={14} />
                {tr('Change username')}
              </button>
              <button className="standard-button" disabled={!!busy} onClick={logout}>
                <LogOut size={14} />
                {tr('Sign out')}
              </button>
            </div>
          )}
        </div>
        <LoggerApiKeys
          key={`${me.username}:${user?.id || ''}`}
          currentId={user?.id}
          connected={!!user && !connection.issue && !!connection.expiresAt && connection.expiresAt > now}
          links={me.links}
          notify={notify}
        />
        <div className="cove-panel login-content">
          <div className="eyebrow">{tr('PROFILE PICTURE')}</div>
          <h3>{tr('Your VRCove picture')}</h3>
          <CovePicture picture={me.picture} username={me.username} size={80} />
          <label className={'standard-button picture-upload' + (busy ? ' disabled' : '')}>
            <Upload size={14} />
            {tr('Upload profile picture')}
            <input
              type="file"
              accept="image/png,image/jpeg,image/gif"
              disabled={!!busy}
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = '';
                if (file) setCropping(file);
              }}
            />
          </label>
          <p className="fineprint">
            {tr(
              'PNG, JPEG or GIF. Before saving, you can move and zoom it to fit the circle. Visible to others when your profile is public.',
            )}
          </p>
          {busy === 'picture' && (
            <span role="status">
              <LoaderCircle size={14} className="spin" />
              {tr('Saving picture…')}
            </span>
          )}
          {cropping && (
            <PictureCropper file={cropping} close={() => setCropping(null)} save={uploadPicture} />
          )}
          {me.picture && (
            <button className="standard-button" disabled={!!busy} onClick={removePicture}>
              <Trash2 size={14} />
              {tr('Remove picture')}
            </button>
          )}
        </div>
        <div className="cove-panel login-content">
          <div className="eyebrow">{tr('PUBLIC PROFILE')}</div>
          <h3>
            {me.public ? <Globe2 size={18} /> : <Lock size={18} />}
            {tr(me.public ? 'Profile is public' : 'Profile is private')}
          </h3>
          <p className="fineprint">
            {me.public
              ? 'Anyone can see your bio, status, every review you’ve posted and your public collections.'
              : 'Only you can see your profile page. Your reviews still show your username where you posted them.'}{' '}
            {tr('Linked VRChat accounts are never shown.')}
          </p>
          <label className="cove-switch">
            <input
              type="checkbox"
              checked={me.public}
              disabled={!!busy}
              onChange={(e) => saveProfile({ public: e.target.checked })}
            />
            <span>{tr('Make my profile public')}</span>
          </label>
          <p className="fineprint">
            {tr('Your bio and status belong to your VRCove profile. They are separate from VRChat.')}
          </p>
          <label>
            {tr('VRCove status')}
            <input
              value={status ?? me.status ?? ''}
              onChange={(e) => setStatus(e.target.value)}
              maxLength={160}
              placeholder={tr('What are you up to?')}
              disabled={!!busy}
            />
            <small>{(status ?? me.status ?? '').length}/160</small>
          </label>
          <label>
            {tr('Bio')}
            <textarea
              value={bio ?? me.bio}
              onChange={(e) => setBio(e.target.value)}
              maxLength={500}
              rows={3}
              placeholder={tr('What you like to review, if you want to say')}
              disabled={!!busy}
            />
            <small>{(bio ?? me.bio).length}/500</small>
          </label>
          <div className="result-actions">
            {((bio !== null && bio !== me.bio) || (status !== null && status !== (me.status ?? ''))) && (
              <button
                className="standard-button"
                disabled={!!busy}
                onClick={() => saveProfile({ bio: bio ?? me.bio, status: status ?? me.status ?? '' })}
              >
                {busy === 'profile' ? <LoaderCircle size={13} className="spin" /> : <Check size={13} />}
                {tr('Save profile')}
              </button>
            )}
            <a className="standard-button" {...appLink(covePath('cove', me.username))}>
              <UserRound size={13} />
              {tr('View my profile')}
            </a>
            <a className="standard-button" {...appLink('/collections')}>
              <Library size={13} />
              {tr('My collections')}
            </a>
          </div>
        </div>
        <div className="cove-panel">
          <div className="eyebrow">{tr('LINKED VRCHAT ACCOUNTS · PRIVATE')}</div>
          <h3>
            <Link2 size={18} />
            {tr('VRChat accounts')}
          </h3>
          <p className="fineprint">
            {tr(
              'These links are private to your account and stored encrypted. The server operator holds the key and can decrypt them. They never appear on public profiles, reviews or collections. A linked account can’t be reviewed by you.',
            )}
          </p>
          {me.links.length ? (
            <ul className="cove-links">
              {me.links.map((l) => {
                const state = vrchatAccountState(l, user?.id, connection, now);
                return (
                  <li key={l.id}>
                    <div>
                      <strong>
                        {l.displayName || 'VRChat account'}{' '}
                        {state === 'active' && <span className="active-account-badge">{tr('Active')}</span>}
                      </strong>
                      <small>
                        {l.vrchatId} {tr('· linked')} {day(l.linked)}
                      </small>
                      <small>
                        {tr(
                          state === 'active'
                            ? 'Active session in this browser'
                            : state === 'available'
                              ? 'Session available in this browser'
                              : 'Reconnect to use this account',
                        )}
                      </small>
                    </div>
                    <div className="linked-account-actions">
                      {state === 'active' ? (
                        <button className="standard-button" disabled>
                          <Check size={13} />
                          {tr('Active')}
                        </button>
                      ) : state === 'available' ? (
                        <button
                          className="standard-button"
                          disabled={!!busy}
                          onClick={() => switchAccount(l.id)}
                        >
                          {busy === 'switch:' + l.id ? (
                            <LoaderCircle size={13} className="spin" />
                          ) : (
                            <ArrowRight size={13} />
                          )}
                          {tr('Use account')}
                        </button>
                      ) : (
                        <button
                          className="standard-button"
                          disabled={!!busy}
                          onClick={() => onLogin(l.vrchatId, l.username)}
                        >
                          {tr('Reconnect')}
                        </button>
                      )}
                      <button className="standard-button" disabled={!!busy} onClick={() => unlink(l.id)}>
                        {busy === 'unlink:' + l.id ? (
                          <LoaderCircle size={13} className="spin" />
                        ) : (
                          <Unlink size={13} />
                        )}
                        {tr('Unlink')}
                      </button>
                    </div>
                    {l.sessions && l.sessions.length > 0 && (
                      <div className="vrchat-sessions">
                        <small>{tr('Connected devices')}</small>
                        <ul>
                          {l.sessions.map((s) => {
                            const Icon = deviceIcons[s.device] || Monitor;
                            return (
                              <li key={s.id}>
                                <Icon size={15} />
                                <div>
                                  <strong>
                                    {tr(deviceNames[s.device] || 'Computer')}
                                    {s.browser ? ' · ' + s.browser : ''}
                                    {s.current && (
                                      <span className="active-account-badge">{tr('This browser')}</span>
                                    )}
                                  </strong>
                                  <small>
                                    {tr('Connected {started} · expires {expires}', {
                                      started: day(s.started),
                                      expires: formatDate(new Date(s.expires).toISOString(), {
                                        year: 'numeric',
                                        month: 'short',
                                        day: 'numeric',
                                      }),
                                    })}
                                  </small>
                                </div>
                                <button
                                  className="standard-button"
                                  disabled={!!busy}
                                  onClick={() => endSession(s)}
                                >
                                  {busy === 'end:' + s.id ? (
                                    <LoaderCircle size={13} className="spin" />
                                  ) : (
                                    <X size={13} />
                                  )}
                                  {tr('End')}
                                </button>
                              </li>
                            );
                          })}
                        </ul>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="fineprint">{tr('No VRChat accounts linked.')}</p>
          )}
          {user && connection.expiresAt && (
            <p className="fineprint">
              {tr('VRChat session expires')}{' '}
              <time dateTime={new Date(connection.expiresAt).toISOString()}>
                {formatDate(new Date(connection.expiresAt).toISOString(), {
                  dateStyle: 'medium',
                  timeStyle: 'short',
                })}
              </time>
              .
            </p>
          )}
          {!user && me.links.length > 0 && (
            <p className="fineprint">
              {tr('Your VRChat accounts are still linked. Reconnect to use VRChat features.')}
            </p>
          )}
          {!user && me.links.length === 0 && (
            <button className="standard-button" onClick={() => onLogin()}>
              {tr('Link a VRChat account')}
            </button>
          )}
          {me.links.length > 0 && (
            <button
              className="standard-button"
              disabled={!!busy || me.links.length >= 10}
              onClick={() => onLogin('')}
            >
              <Link2 size={14} />
              {tr('Link another VRChat account')}
            </button>
          )}
          <p className="fineprint">
            {tr(
              'You can link up to 10 VRChat accounts. Saved sessions stay encrypted in this browser. Switching makes that account active for all VRChat actions in this browser.',
            )}
          </p>
          <p className="fineprint">
            {tr(
              'Sign in to VRChat to verify ownership and link it automatically. VRChat sees a sign-in through VRCove but not your VRCove username.',
            )}
          </p>
        </div>
        <div className="cove-panel login-content">
          <div className="eyebrow">{tr('ACCOUNT RECOVERY')}</div>
          <h3>
            <KeyRound size={18} />
            {tr('Recovery codes')}
          </h3>
          <p className="fineprint">
            {tr('{count} unused recovery codes remaining.', { count: me.recoveryCodesRemaining || 0 })}{' '}
            {tr(
              'Each code can reset your password once. Store them somewhere safe; anyone with a code and your username can access your account.',
            )}
          </p>
          {codes.length > 0 ? (
            <div role="region" aria-label={tr('Your new recovery codes')}>
              <p className="fineprint">
                {tr('Save these codes now. They will not be shown again. All previous codes are invalid.')}
              </p>
              <ul className="recovery-codes">
                {codes.map((code) => (
                  <li key={code}>
                    <code>{code}</code>
                  </li>
                ))}
              </ul>
              <button className="standard-button" onClick={downloadCodes}>
                {tr('Download codes')}
              </button>
              <button className="standard-button" onClick={() => setCodes([])}>
                {tr('I saved my codes')}
              </button>
            </div>
          ) : (
            <form onSubmit={generateCodes}>
              <label>
                {tr('Confirm password for recovery codes')}
                <input
                  type="password"
                  autoComplete="current-password"
                  required
                  maxLength={1024}
                  value={recoveryPassword}
                  onChange={(e) => setRecoveryPassword(e.target.value)}
                />
              </label>
              <p className="fineprint">
                {tr('Generating a new set immediately invalidates all previous codes.')}
              </p>
              <button className="standard-button" type="submit" disabled={!!busy}>
                {busy === 'recovery' ? <LoaderCircle size={13} className="spin" /> : <KeyRound size={13} />}
                {tr(me.recoveryCodesRemaining ? 'Replace recovery codes' : 'Generate recovery codes')}
              </button>
            </form>
          )}
        </div>
        <form className="cove-panel login-content" onSubmit={changePassword}>
          <div className="eyebrow">{tr('SECURITY')}</div>
          <h3>
            <KeyRound size={18} />
            {tr('Change password')}
          </h3>
          <label>
            {tr('Current password')}
            <input
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
              required
              maxLength={1024}
            />
          </label>
          <label>
            {tr('New password')}
            <input
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              autoComplete="new-password"
              required
              minLength={10}
              maxLength={1024}
            />
          </label>
          <button className="standard-button" type="submit" disabled={!!busy}>
            {busy === 'password' ? <LoaderCircle size={13} className="spin" /> : <Check size={13} />}
            {tr('Change password')}
          </button>
        </form>
        <div className="cove-panel login-content">
          <div className="eyebrow">{tr('YOUR DATA')}</div>
          <h3>
            <Download size={18} />
            {tr('Download your data')}
          </h3>
          <p className="fineprint">
            {tr(
              'Get a JSON file with everything VRCove stores about your account: profile, picture, VRChat links, devices, reviews, collections and reports you sent. Passwords and keys aren’t included.',
            )}
          </p>
          {exporting ? (
            <form onSubmit={downloadData}>
              <label>
                {tr('Confirm with your password')}
                <input
                  type="password"
                  value={exportPassword}
                  onChange={(e) => setExportPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                  maxLength={1024}
                />
              </label>
              <button className="standard-button" type="submit" disabled={!!busy}>
                {busy === 'export' ? <LoaderCircle size={13} className="spin" /> : <Download size={13} />}
                {tr('Download')}
              </button>
              <button type="button" className="standard-button" onClick={() => setExporting(false)}>
                {tr('Cancel')}
              </button>
            </form>
          ) : (
            <button className="standard-button" onClick={() => setExporting(true)}>
              {tr('Download my data')}
            </button>
          )}
        </div>
        <div className="cove-panel login-content">
          <div className="eyebrow">{tr('DANGER ZONE')}</div>
          <h3>
            <Trash2 size={18} />
            {tr('Delete account')}
          </h3>
          <p className="fineprint">
            {tr(
              'Deletes your account, all your reviews, collections and every VRChat link. This can’t be undone.',
            )}
          </p>
          {deleting ? (
            <form onSubmit={remove}>
              <label>
                {tr('Confirm with your password')}
                <input
                  type="password"
                  value={deletePassword}
                  onChange={(e) => setDeletePassword(e.target.value)}
                  autoComplete="current-password"
                  required
                  maxLength={1024}
                />
              </label>
              <button className="standard-button danger-text" type="submit" disabled={!!busy}>
                {busy === 'delete' ? <LoaderCircle size={13} className="spin" /> : <Trash2 size={13} />}
                {tr('Delete permanently')}
              </button>
              <button type="button" className="standard-button" onClick={() => setDeleting(false)}>
                {tr('Cancel')}
              </button>
            </form>
          ) : (
            <button className="standard-button danger-text" onClick={() => setDeleting(true)}>
              {tr('Delete my account')}
            </button>
          )}
        </div>
      </div>
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      <NameAdvice />
    </section>
  );
}
