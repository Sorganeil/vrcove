import { useEffect, useState } from 'react';
import { LoaderCircle, Check } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import Modal from './modal';

// Edits the VRChat status and bio of the connected account.
export default function ProfileEditor({ user, close, updated }: any) {
  useLocale();
  const [bio, setBio] = useState(user.bio || ''),
    [status, setStatus] = useState(
      ['active', 'join me', 'ask me', 'busy'].includes(user.status) ? user.status : 'active',
    ),
    [description, setDescription] = useState(user.statusDescription || ''),
    [saving, setSaving] = useState(false),
    [error, setError] = useState(''),
    [loaded, setLoaded] = useState(false);
  useEffect(() => {
    let alive = true;
    api('/account/user?id=' + user.id)
      .then((profile) => {
        if (!alive) return;
        setBio(profile.bio || '');
        setStatus(
          ['active', 'join me', 'ask me', 'busy'].includes(profile.status) ? profile.status : 'active',
        );
        setDescription(profile.statusDescription || '');
        setLoaded(true);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [user.id]);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!loaded) return;
    setSaving(true);
    setError('');
    try {
      const next = await api('/account/profile', { bio, status, statusDescription: description });
      updated(next);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal title={tr('Edit profile')} close={close}>
      <div className="login-content">
        <div className="eyebrow">{tr('YOUR VRCHAT PROFILE')}</div>
        <h2>{tr('Edit profile')}</h2>
        {!loaded && !error && (
          <div className="loading-inline">
            <LoaderCircle size={14} className="spin" />
            {tr('Loading…')}
          </div>
        )}
        <form onSubmit={submit}>
          <label>
            {tr('Social status')}
            <select value={status} onChange={(e) => setStatus(e.target.value)} disabled={saving || !loaded}>
              <option value="active">{tr('Online')}</option>
              <option value="join me">{tr('Join me')}</option>
              <option value="ask me">{tr('Ask me')}</option>
              <option value="busy">{tr('Do not disturb')}</option>
            </select>
          </label>
          <label>
            {tr('Status message')}
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              maxLength={32}
              disabled={saving || !loaded}
            />
            <small>{description.length}/32</small>
          </label>
          <label className="notes-panel">
            {tr('Bio')}
            <textarea
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              maxLength={512}
              disabled={saving || !loaded}
              rows={5}
            />
            <small>{bio.length}/512</small>
          </label>
          {error && (
            <div className="form-error" role="alert">
              {error}
            </div>
          )}
          <button type="submit" className="primary" disabled={saving || !loaded}>
            {saving ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />}
            {tr('Save changes')}
          </button>
          <button type="button" className="standard-button" onClick={close} disabled={saving}>
            {tr('Cancel')}
          </button>
        </form>
      </div>
    </Modal>
  );
}
