import { useState } from 'react';
import { Bell, Check, ExternalLink, LoaderCircle, Users } from 'lucide-react';
import { api } from '../lib/api';
import { formatDate, t as tr, useLocale } from '../lib/i18n';
import { useApp } from '../lib/app-context';
import { useLoader } from '../lib/use-loader';
import Loading from '../components/loading';
import Modal from '../components/modal';
import { ErrorBanner, RequireVRChat, SectionPage } from '../components/section-page';

/** Recent VRChat notifications, with friend request and dismiss actions. */
export default function Notifications() {
  return (
    <SectionPage title="Notifications" subtitle="Recent notifications from your VRChat account.">
      <RequireVRChat section="Notifications">
        <NotificationList />
      </RequireVRChat>
    </SectionPage>
  );
}

function NotificationList() {
  useLocale();
  const { notify } = useApp();
  const [busy, setBusy] = useState('');
  const [acceptDialog, setAcceptDialog] = useState<any>(null);
  const { data, setData, loading, error, retry } = useLoader<any[]>(
    () => api('/account/notifications'),
    [],
    [] as any[],
  );

  async function act(action: 'accept' | 'hide' | 'see', n: any) {
    setBusy(action + ':' + n.id);
    try {
      await api('/account/action', { action, id: n.id });
      setData((rows) =>
        action === 'see'
          ? rows.map((row) => (row.id === n.id ? { ...row, seen: true } : row))
          : rows.filter((row) => row.id !== n.id),
      );
      setAcceptDialog(null);
      notify(
        action === 'accept'
          ? 'Friend request accepted'
          : action === 'hide'
            ? 'Notification hidden'
            : 'Notification marked as seen',
      );
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  return (
    <>
      <ErrorBanner error={error} retry={retry} />
      {loading ? (
        <Loading text={tr('Checking notifications…')} />
      ) : (
        <div className="notification-list">
          {data.length ? (
            data.map((n) => (
              <article key={n.id}>
                <Bell size={20} />
                <div>
                  <h3>
                    {n.senderUsername || 'VRChat'}
                    <span>{n.type}</span>
                  </h3>
                  <p>{n.message || 'Open VRChat to view this notification.'}</p>
                  <small>
                    {formatDate(n.created_at)}
                    {n.seen ? ' · Seen' : ''}
                  </small>
                  <div className="result-actions">
                    {n.type === 'friendRequest' && (
                      <button
                        className="standard-button"
                        disabled={!!busy}
                        onClick={() => setAcceptDialog(n)}
                      >
                        {tr('Accept request')}
                      </button>
                    )}
                    {!n.seen && (
                      <button className="standard-button" disabled={!!busy} onClick={() => act('see', n)}>
                        {tr('Mark seen')}
                      </button>
                    )}
                    <button className="standard-button" disabled={!!busy} onClick={() => act('hide', n)}>
                      {tr('Hide')}
                    </button>
                    {busy.endsWith(':' + n.id) && <LoaderCircle size={14} className="spin" />}
                  </div>
                </div>
                <a
                  className="icon-button"
                  href="https://vrchat.com/home"
                  target="_blank"
                  rel="noreferrer"
                  aria-label={tr('View in VRChat')}
                >
                  <ExternalLink size={16} />
                </a>
              </article>
            ))
          ) : (
            <div className="empty-state">
              <Check />
              <h3>{tr('All caught up.')}</h3>
              <p>{tr('No recent notifications.')}</p>
            </div>
          )}
        </div>
      )}
      {acceptDialog && (
        <Modal title={tr('Accept friend request')} close={() => setAcceptDialog(null)}>
          <div className="confirm-content">
            <Users size={28} />
            <h2>{tr('Accept friend request?')}</h2>
            <p>
              {tr('Add')} <strong>{acceptDialog.senderUsername || 'this person'}</strong>{' '}
              {tr('to your VRChat friends?')}
            </p>
            <button className="primary" disabled={!!busy} onClick={() => act('accept', acceptDialog)}>
              {busy ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />}
              {tr('Accept request')}
            </button>
            <button disabled={!!busy} onClick={() => setAcceptDialog(null)}>
              {tr('Cancel')}
            </button>
          </div>
        </Modal>
      )}
    </>
  );
}
