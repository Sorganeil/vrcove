import { useState } from 'react';
import {
  ArrowUpRight,
  Ban,
  Check,
  Flag,
  LoaderCircle,
  Search,
  ShieldCheck,
  ShieldOff,
  Trash2,
  X,
} from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useResource } from '../lib/use-resource';
import { appLink, covePath, entityPath, type EntityKind } from '../lib/routes';
import { reportReasons } from '../components/moderation';
import { Stars } from '../components/reviews';

const tabs = [
  ['reports', 'Reports'],
  ['requests', 'Removal requests'],
  ['users', 'Users'],
  ['activity', 'Activity'],
] as const;
type Tab = (typeof tabs)[number][0];

const suspensions = [
  ['1', '1 day'],
  ['3', '3 days'],
  ['7', '1 week'],
  ['30', '1 month'],
  ['90', '3 months'],
  ['365', '1 year'],
] as const;
const when = (seconds: number) => new Date(seconds * 1000).toLocaleString(document.documentElement.lang);
const reasonLabel = (value: string) => tr(reportReasons.find(([v]) => v === value)?.[1] || value);

/* Hidden moderation panel for admins and owners: the report and removal backlogs, users and recent actions. */
export default function Admin({ notify }: { notify: (s: string) => void }) {
  useLocale();
  const summary = useResource('/cove/admin/summary');
  const [tab, setTab] = useState<Tab>('reports');
  if (summary.status === 404 || summary.status === 401)
    return <p className="fineprint">{tr('This page doesn’t exist.')}</p>;
  if (!summary.data)
    return summary.error ? (
      <p className="form-error">{summary.error}</p>
    ) : (
      <div className="loading-inline">
        <LoaderCircle size={14} className="spin" />
        {tr('Loading…')}
      </div>
    );
  const s = summary.data;
  const counts: Record<Tab, number | null> = {
    reports: s.openReports,
    requests: s.openRequests,
    users: s.users,
    activity: null,
  };
  return (
    <section className="admin-page" aria-labelledby="admin-title">
      <div className="section-heading">
        <div>
          <h2 id="admin-title">
            <ShieldCheck size={20} />
            {tr('Moderation')}
          </h2>
          <p>
            {tr(s.role === 'owner' ? 'Signed in as an owner.' : 'Signed in as an admin.')}{' '}
            {tr('{banned} banned · {suspended} suspended', { banned: s.banned, suspended: s.suspended })}
          </p>
        </div>
      </div>
      <div className="admin-tabs" role="tablist">
        {tabs.map(([value, label]) => (
          <button
            key={value}
            role="tab"
            aria-selected={tab === value}
            className={tab === value ? 'active' : ''}
            onClick={() => setTab(value)}
          >
            {tr(label)}
            {counts[value] !== null && <small>{counts[value]}</small>}
          </button>
        ))}
      </div>
      {tab === 'reports' && <Reports notify={notify} changed={summary.reload} />}
      {tab === 'requests' && <Requests notify={notify} changed={summary.reload} />}
      {tab === 'users' && <Users notify={notify} role={s.role} changed={summary.reload} />}
      {tab === 'activity' && (
        <ol className="admin-list admin-log">
          {s.log.length === 0 && <li className="fineprint">{tr('No moderation actions yet.')}</li>}
          {s.log.map((e: any, i: number) => (
            <li key={i}>
              <strong>{e.actor}</strong> {e.action} <code>{e.target}</code>
              <small>{when(e.time)}</small>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function StatusToggle({ closed, setClosed }: { closed: boolean; setClosed: (v: boolean) => void }) {
  useLocale();
  return (
    <div className="admin-filter">
      <button className={!closed ? 'active' : ''} onClick={() => setClosed(false)}>
        {tr('Open')}
      </button>
      <button className={closed ? 'active' : ''} onClick={() => setClosed(true)}>
        {tr('Handled')}
      </button>
    </div>
  );
}

function useAction(notify: (s: string) => void, done: () => void) {
  const [busy, setBusy] = useState('');
  async function run(key: string, path: string, body: object, message: string) {
    setBusy(key);
    try {
      await api(path, body);
      notify(message);
      done();
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  return { busy, run };
}

function Reports({ notify, changed }: { notify: (s: string) => void; changed: () => void }) {
  useLocale();
  const [closed, setClosed] = useState(false);
  const list = useResource('/cove/admin/reports' + (closed ? '?status=closed' : ''));
  const { busy, run } = useAction(notify, () => {
    list.reload();
    changed();
  });
  const act = (id: number, action: string, message: string) =>
    run(id + action, '/cove/admin/report', { id, action }, message);
  return (
    <>
      <StatusToggle closed={closed} setClosed={setClosed} />
      {!list.data ? (
        <p className="fineprint">{list.error || tr('Loading…')}</p>
      ) : list.data.reports.length === 0 ? (
        <p className="fineprint">{closed ? tr('No handled reports.') : tr('No open reports. Nice.')}</p>
      ) : (
        <div className="admin-list">
          {list.data.reports.map((r: any) => (
            <article className="admin-card" key={r.id}>
              <header>
                <Flag size={14} />
                <strong>{r.kind === 'review' ? tr('Review') : tr('Profile')}</strong>
                <span className="admin-pill">{reasonLabel(r.reason)}</span>
                <small>
                  {tr('by {name}', { name: r.reporter || tr('deleted account') })} · {when(r.created)}
                </small>
              </header>
              {r.kind === 'review' ? (
                <blockquote>
                  <div>
                    <a className="text-link" {...appLink(covePath('cove', r.content.author))}>
                      {r.content.author}
                    </a>{' '}
                    {tr('on')}{' '}
                    <a
                      className="text-link"
                      {...appLink(entityPath(r.content.kind as EntityKind, r.content.target))}
                    >
                      {r.content.name || r.content.target}
                    </a>{' '}
                    <Stars value={r.content.rating} size={12} />
                  </div>
                  {r.content.body ? (
                    <p>{r.content.body}</p>
                  ) : (
                    <p className="fineprint">{tr('No comment.')}</p>
                  )}
                  {!r.reviewExists && <small>{tr('The review has since been deleted.')}</small>}
                </blockquote>
              ) : (
                <blockquote>
                  <a className="text-link" {...appLink(covePath('cove', r.content.username))}>
                    {r.content.username}
                    <ArrowUpRight size={12} />
                  </a>
                  {r.content.status && <p>{r.content.status}</p>}
                  {r.content.bio && <p>{r.content.bio}</p>}
                </blockquote>
              )}
              {r.details && (
                <p className="admin-note">
                  <strong>{tr('Reporter’s note:')}</strong> {r.details}
                </p>
              )}
              {r.status === 'open' ? (
                <div className="result-actions">
                  {r.kind === 'review' && r.reviewExists && (
                    <button
                      className="standard-button danger-text"
                      disabled={!!busy}
                      onClick={() => act(r.id, 'delete-review', tr('Review deleted'))}
                    >
                      <Trash2 size={13} />
                      {tr('Delete review')}
                    </button>
                  )}
                  <button
                    className="standard-button"
                    disabled={!!busy}
                    onClick={() => act(r.id, 'resolve', tr('Report resolved'))}
                  >
                    <Check size={13} />
                    {tr('Mark resolved')}
                  </button>
                  <button
                    className="standard-button"
                    disabled={!!busy}
                    onClick={() => act(r.id, 'dismiss', tr('Report dismissed'))}
                  >
                    <X size={13} />
                    {tr('Dismiss')}
                  </button>
                  {r.account && (
                    <span className="fineprint">{tr('Manage {name} in Users.', { name: r.account })}</span>
                  )}
                </div>
              ) : (
                <p className="fineprint">
                  {r.status} · {r.resolution} · {r.handledBy}
                </p>
              )}
            </article>
          ))}
        </div>
      )}
    </>
  );
}

/* Lets staff take any avatar out of the catalog, such as spam, without waiting for a request. */
function RemoveAvatar({ notify, changed }: { notify: (s: string) => void; changed: () => void }) {
  useLocale();
  const [target, setTarget] = useState(''),
    [reason, setReason] = useState(''),
    [busy, setBusy] = useState(false);
  // Accept a pasted avatar link as well as a bare ID.
  const id =
    target.match(/avtr_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i)?.[0].toLowerCase() ||
    '';
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!id || !confirm(tr('Remove this avatar from VRCove and block it from being added again?'))) return;
    setBusy(true);
    try {
      const result = await api('/cove/admin/avatar/remove', { avatarId: id, reason: reason.trim() });
      notify(tr(result.removed ? 'Avatar removed' : 'This avatar wasn’t in the catalog. It is now blocked.'));
      setTarget('');
      setReason('');
      changed();
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="admin-card admin-remove" onSubmit={submit} aria-labelledby="admin-remove-title">
      <header>
        <Trash2 size={14} />
        <strong id="admin-remove-title">{tr('Remove an avatar')}</strong>
      </header>
      <p className="fineprint">
        {tr(
          'Removes it from search and blocks the desktop companion from adding it again. Use it for spam or abuse.',
        )}
      </p>
      <label className="notes-panel">
        <span>{tr('Avatar ID or link')}</span>
        <input value={target} onChange={(e) => setTarget(e.target.value)} placeholder="avtr_…" required />
      </label>
      {target.trim() && !id && <p className="form-error">{tr('That isn’t an avatar ID or link.')}</p>}
      <label className="notes-panel">
        <span>{tr('Reason')}</span>
        <input
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          maxLength={200}
          minLength={3}
          required
        />
      </label>
      <div className="result-actions">
        <button className="primary" type="submit" disabled={busy || !id || reason.trim().length < 3}>
          {busy ? <LoaderCircle size={13} className="spin" /> : <Trash2 size={13} />}
          {tr('Remove avatar')}
        </button>
      </div>
    </form>
  );
}

function Requests({ notify, changed }: { notify: (s: string) => void; changed: () => void }) {
  useLocale();
  const [closed, setClosed] = useState(false),
    [notes, setNotes] = useState<Record<number, string>>({});
  const list = useResource('/cove/admin/requests' + (closed ? '?status=closed' : ''));
  const { busy, run } = useAction(notify, () => {
    list.reload();
    changed();
  });
  return (
    <>
      <RemoveAvatar
        notify={notify}
        changed={() => {
          list.reload();
          changed();
        }}
      />
      <StatusToggle closed={closed} setClosed={setClosed} />
      {!list.data ? (
        <p className="fineprint">{list.error || tr('Loading…')}</p>
      ) : list.data.requests.length === 0 ? (
        <p className="fineprint">{closed ? tr('No handled requests.') : tr('No open removal requests.')}</p>
      ) : (
        <div className="admin-list">
          {list.data.requests.map((q: any) => (
            <article className="admin-card" key={q.id}>
              <header>
                <ShieldOff size={14} />
                <a className="text-link" {...appLink(entityPath('avatar', q.avatarId))}>
                  {q.avatarName || q.avatarId}
                </a>
                {q.verified ? (
                  <span className="admin-pill good">{tr('Verified owner')}</span>
                ) : (
                  <span className="admin-pill">{tr('Unverified')}</span>
                )}
                <small>
                  {tr('by {name}', { name: q.requester || tr('deleted account') })} · {when(q.created)}
                </small>
              </header>
              <code className="admin-id">{q.avatarId}</code>
              {q.verified && (
                <p className="fineprint">
                  {tr('The requester’s connected VRChat account is the avatar’s author.')}
                </p>
              )}
              {q.proof && <blockquote>{q.proof}</blockquote>}
              {q.status === 'open' ? (
                <>
                  <label className="notes-panel">
                    <span>{tr('Note (optional)')}</span>
                    <input
                      value={notes[q.id] || ''}
                      maxLength={1000}
                      onChange={(e) => setNotes({ ...notes, [q.id]: e.target.value })}
                    />
                  </label>
                  <div className="result-actions">
                    <button
                      className="primary"
                      disabled={!!busy}
                      onClick={() =>
                        confirm(tr('Remove this avatar from VRCove and block it from being added again?')) &&
                        run(
                          q.id + 'approve',
                          '/cove/admin/request',
                          { id: q.id, action: 'approve', note: notes[q.id] || '' },
                          tr('Avatar removed'),
                        )
                      }
                    >
                      <Check size={13} />
                      {tr('Approve and remove')}
                    </button>
                    <button
                      className="standard-button"
                      disabled={!!busy}
                      onClick={() =>
                        run(
                          q.id + 'reject',
                          '/cove/admin/request',
                          { id: q.id, action: 'reject', note: notes[q.id] || '' },
                          tr('Request rejected'),
                        )
                      }
                    >
                      <X size={13} />
                      {tr('Reject')}
                    </button>
                  </div>
                </>
              ) : (
                <p className="fineprint">
                  {q.status} · {q.handledBy}
                  {q.note ? ' · ' + q.note : ''}
                </p>
              )}
            </article>
          ))}
        </div>
      )}
    </>
  );
}

function Users({
  notify,
  role,
  changed,
}: {
  notify: (s: string) => void;
  role: string;
  changed: () => void;
}) {
  useLocale();
  const [query, setQuery] = useState(''),
    [search, setSearch] = useState(''),
    [offset, setOffset] = useState(0),
    [days, setDays] = useState<Record<number, string>>({});
  const list = useResource('/cove/admin/users?q=' + encodeURIComponent(search) + '&offset=' + offset);
  const { busy, run } = useAction(notify, () => {
    list.reload();
    changed();
  });
  const rank = (r: string) => (r === 'owner' ? 2 : r === 'admin' ? 1 : 0);
  const act = (u: any, action: string, message: string, extra: object = {}) =>
    run(u.id + action, '/cove/admin/user', { id: u.id, action, ...extra }, message);
  return (
    <>
      <form
        className="admin-search"
        onSubmit={(e) => {
          e.preventDefault();
          setOffset(0);
          setSearch(query.trim());
        }}
      >
        <Search size={15} />
        <input
          aria-label={tr('Search usernames')}
          placeholder={tr('Search usernames')}
          value={query}
          maxLength={64}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button className="standard-button" type="submit">
          {tr('Search')}
        </button>
      </form>
      {!list.data ? (
        <p className="fineprint">{list.error || tr('Loading…')}</p>
      ) : (
        <>
          <p className="fineprint">{tr('{count} accounts', { count: list.data.total })}</p>
          <div className="admin-list">
            {list.data.users.map((u: any) => {
              const manageable = rank(u.role) < rank(role);
              return (
                <article className="admin-card admin-user" key={u.id}>
                  <header>
                    {u.public ? (
                      <a className="text-link" {...appLink(covePath('cove', u.username))}>
                        {u.username}
                      </a>
                    ) : (
                      <strong>{u.username}</strong>
                    )}
                    {u.role !== 'user' && (
                      <span className="admin-pill good">{tr(u.role === 'owner' ? 'Owner' : 'Admin')}</span>
                    )}
                    {u.banned && <span className="admin-pill bad">{tr('Banned')}</span>}
                    {u.suspendedUntil > 0 && (
                      <span className="admin-pill bad">
                        {tr('Suspended until {date}', { date: when(u.suspendedUntil) })}
                      </span>
                    )}
                    <small>
                      {tr('Joined {date}', { date: u.created })} ·{' '}
                      {tr(u.reviews === 1 ? '1 review' : '{count} reviews', { count: u.reviews })}
                      {u.openReports > 0 && ' · ' + tr('{count} open reports', { count: u.openReports })}
                    </small>
                  </header>
                  {manageable && (
                    <div className="result-actions">
                      {role === 'owner' &&
                        (u.role === 'admin' ? (
                          <button
                            className="standard-button"
                            disabled={!!busy}
                            onClick={() => act(u, 'make-user', tr('Admin removed'))}
                          >
                            {tr('Remove admin')}
                          </button>
                        ) : (
                          <button
                            className="standard-button"
                            disabled={!!busy}
                            onClick={() => act(u, 'make-admin', tr('Promoted to admin'))}
                          >
                            <ShieldCheck size={13} />
                            {tr('Make admin')}
                          </button>
                        ))}
                      {u.suspendedUntil > 0 ? (
                        <button
                          className="standard-button"
                          disabled={!!busy}
                          onClick={() => act(u, 'unsuspend', tr('Suspension lifted'))}
                        >
                          {tr('Lift suspension')}
                        </button>
                      ) : (
                        <span className="admin-suspend">
                          <label>
                            {tr('Suspend for')}
                            <select
                              value={days[u.id] || '7'}
                              onChange={(e) => setDays({ ...days, [u.id]: e.target.value })}
                            >
                              {suspensions.map(([n, label]) => (
                                <option key={n} value={n}>
                                  {tr(label)}
                                </option>
                              ))}
                            </select>
                          </label>
                          <button
                            className="standard-button"
                            disabled={!!busy}
                            onClick={() =>
                              act(u, 'suspend', tr('Account suspended'), { days: Number(days[u.id] || 7) })
                            }
                          >
                            {tr('Suspend')}
                          </button>
                        </span>
                      )}
                      {u.banned ? (
                        <button
                          className="standard-button"
                          disabled={!!busy}
                          onClick={() => act(u, 'unban', tr('Ban lifted'))}
                        >
                          {tr('Unban')}
                        </button>
                      ) : (
                        <button
                          className="standard-button danger-text"
                          disabled={!!busy}
                          onClick={() =>
                            confirm(
                              tr('Ban {name}? Their reviews and profile will be hidden.', {
                                name: u.username,
                              }),
                            ) && act(u, 'ban', tr('Account banned'))
                          }
                        >
                          <Ban size={13} />
                          {tr('Ban')}
                        </button>
                      )}
                      <button
                        className="standard-button danger-text"
                        disabled={!!busy}
                        onClick={() =>
                          confirm(
                            tr('Permanently delete {name} with all their reviews and collections?', {
                              name: u.username,
                            }),
                          ) && act(u, 'delete', tr('Account deleted'))
                        }
                      >
                        <Trash2 size={13} />
                        {tr('Delete')}
                      </button>
                    </div>
                  )}
                </article>
              );
            })}
          </div>
          {(offset > 0 || offset + list.data.pageSize < list.data.total) && (
            <div className="result-actions">
              <button
                className="standard-button"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - list.data.pageSize))}
              >
                {tr('Previous')}
              </button>
              <button
                className="standard-button"
                disabled={offset + list.data.pageSize >= list.data.total}
                onClick={() => setOffset(offset + list.data.pageSize)}
              >
                {tr('Next')}
              </button>
            </div>
          )}
        </>
      )}
    </>
  );
}
