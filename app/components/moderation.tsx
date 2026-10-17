import { useState } from 'react';
import { Check, Flag, LoaderCircle, ShieldOff } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { appLink } from '../lib/routes';
import Modal from './modal';

export const reportReasons: [string, string][] = [
  ['spam', 'Spam or advertising'],
  ['harassment', 'Harassment or bullying'],
  ['hate', 'Hate speech'],
  ['sexual', 'Sexual content'],
  ['personal-info', 'Shares personal information'],
  ['impersonation', 'Impersonation'],
  ['other', 'Something else'],
];

type ReportTarget = { kind: 'review'; id: number } | { kind: 'profile'; username: string };

/* Report a review or profile to VRCove moderators. Hidden when signed out. */
export function ReportButton({ target, className = '' }: { target: ReportTarget; className?: string }) {
  useLocale();
  const { coveAccount } = useApp();
  const [open, setOpen] = useState(false),
    [reason, setReason] = useState(''),
    [details, setDetails] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [sent, setSent] = useState(false);
  if (!coveAccount) return null;
  const label = target.kind === 'review' ? tr('Report review') : tr('Report profile');
  async function send(e: React.FormEvent) {
    e.preventDefault();
    if (!reason) {
      setError(tr('Choose a reason.'));
      return;
    }
    setBusy(true);
    setError('');
    try {
      await api('/cove/report', { ...target, reason, details });
      setSent(true);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <button className={'report-button ' + className} onClick={() => setOpen(true)} aria-label={label}>
        <Flag size={13} />
        <span>{tr('Report')}</span>
      </button>
      {open && (
        <Modal title={label} close={() => setOpen(false)}>
          <div className="confirm-content moderation-dialog">
            <h2>{label}</h2>
            {sent ? (
              <>
                <p>
                  <Check size={16} /> {tr('Thanks. Moderators will review your report.')}
                </p>
                <button className="standard-button" onClick={() => setOpen(false)}>
                  {tr('Close')}
                </button>
              </>
            ) : (
              <form onSubmit={send}>
                <fieldset>
                  <legend>{tr('Reason')}</legend>
                  {reportReasons.map(([value, text]) => (
                    <label key={value} className="moderation-choice">
                      <input
                        type="radio"
                        name="report-reason"
                        value={value}
                        checked={reason === value}
                        onChange={() => setReason(value)}
                      />
                      {tr(text)}
                    </label>
                  ))}
                </fieldset>
                <label className="notes-panel">
                  <span>{tr('Details (optional)')}</span>
                  <textarea
                    value={details}
                    onChange={(e) => setDetails(e.target.value)}
                    maxLength={1000}
                    rows={3}
                    disabled={busy}
                  />
                </label>
                {error && (
                  <div className="form-error" role="alert">
                    {error}
                  </div>
                )}
                <div className="result-actions">
                  <button className="primary" type="submit" disabled={busy}>
                    {busy ? <LoaderCircle size={14} className="spin" /> : <Flag size={14} />}
                    {tr('Send report')}
                  </button>
                  <button type="button" className="standard-button" onClick={() => setOpen(false)}>
                    {tr('Cancel')}
                  </button>
                </div>
              </form>
            )}
          </div>
        </Modal>
      )}
    </>
  );
}

/* Lets an avatar's creator ask for it to be removed from VRCove and kept out. */
export function RemovalRequest({ avatarId, name }: { avatarId: string; name: string }) {
  useLocale();
  const { coveAccount } = useApp();
  const [open, setOpen] = useState(false),
    [proof, setProof] = useState(''),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [sent, setSent] = useState<null | boolean>(null);
  async function send(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const out = await api('/cove/removal-request', { avatarId, proof });
      setSent(!!out.verified);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <button className="text-button removal-link" onClick={() => setOpen(true)}>
        <ShieldOff size={13} />
        {tr('Your avatar? Request removal')}
      </button>
      {open && (
        <Modal title={tr('Request removal')} close={() => setOpen(false)}>
          <div className="confirm-content moderation-dialog">
            <h2>{tr('Request removal')}</h2>
            <p>
              {tr(
                'If you made {name}, moderators can remove it from VRCove and stop it from being added again.',
                { name },
              )}
            </p>
            {!coveAccount ? (
              <p>
                <a className="text-link" {...appLink('/account')}>
                  {tr('Sign in with a VRCove account')}
                </a>{' '}
                {tr('to request removal.')}
              </p>
            ) : sent !== null ? (
              <>
                <p>
                  <Check size={16} />{' '}
                  {sent
                    ? tr(
                        'Request sent. Your connected VRChat account uploaded this avatar, so moderators can see it’s yours.',
                      )
                    : tr('Request sent. Moderators will check your proof.')}
                </p>
                <button className="standard-button" onClick={() => setOpen(false)}>
                  {tr('Close')}
                </button>
              </>
            ) : (
              <form onSubmit={send}>
                <p className="fineprint">
                  {tr(
                    'Fastest: connect the VRChat account that uploaded this avatar, then send this request. Otherwise, explain how moderators can confirm it’s yours, such as a link to where you publish or sell it.',
                  )}
                </p>
                <label className="notes-panel">
                  <span>{tr('Proof of ownership')}</span>
                  <textarea
                    value={proof}
                    onChange={(e) => setProof(e.target.value)}
                    maxLength={2000}
                    rows={4}
                    disabled={busy}
                  />
                </label>
                {error && (
                  <div className="form-error" role="alert">
                    {error}
                  </div>
                )}
                <div className="result-actions">
                  <button className="primary" type="submit" disabled={busy}>
                    {busy ? <LoaderCircle size={14} className="spin" /> : <ShieldOff size={14} />}
                    {tr('Send request')}
                  </button>
                  <button type="button" className="standard-button" onClick={() => setOpen(false)}>
                    {tr('Cancel')}
                  </button>
                </div>
              </form>
            )}
          </div>
        </Modal>
      )}
    </>
  );
}
