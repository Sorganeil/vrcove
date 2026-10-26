import { useEffect, useState } from 'react';
import { Search } from 'lucide-react';
import { api } from '../lib/api';
import { formatDate, t as tr, useLocale } from '../lib/i18n';
import { detailLink } from '../components/detail-links';
import Modal from '../components/modal';
import { SectionPage } from '../components/section-page';

// Pages that only exist in the desktop app. Their data stays on this computer:
// the app reads VRChat's logs and live feed and keeps the history locally.

type Encounter = { userId?: string; displayName: string; joined: number; left?: number; avatar?: string };
type Visit = {
  id: number;
  location: string;
  worldId: string;
  worldName: string;
  joined: number;
  left?: number;
  people: Encounter[];
};
type FeedEntry = {
  id: number;
  at: number;
  userId: string;
  displayName: string;
  kind: string;
  detail?: string;
  previous?: string;
  worldName?: string;
};
type Shot = {
  file: string;
  taken: number;
  width?: number;
  height?: number;
  worldId?: string;
  worldName?: string;
  people: Encounter[];
};

const when = (ms: number) =>
  formatDate(new Date(ms).toISOString(), {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
const clock = (ms: number) => formatDate(new Date(ms).toISOString(), { hour: 'numeric', minute: '2-digit' });

function Person({ p }: { p: Pick<Encounter, 'userId' | 'displayName'> }) {
  return p.userId ? <a {...detailLink('user', p.userId)}>{p.displayName}</a> : <span>{p.displayName}</span>;
}

function World({ id, name }: { id?: string; name?: string }) {
  if (!name) return <span>{tr('a private instance')}</span>;
  return id ? <a {...detailLink('world', id)}>{name}</a> : <span>{name}</span>;
}

/** Loads pages of a list that is ordered newest first, `before` a time. */
function usePages<T>(
  path: (before: number) => string,
  key: (data: any) => T[],
  at: (item: T) => number,
  deps: unknown[],
) {
  const [items, setItems] = useState<T[] | null>(null);
  const [more, setMore] = useState(false);
  const [error, setError] = useState('');
  const load = (before: number, reset: boolean) =>
    api(path(before))
      .then((data) => {
        const page = key(data);
        setItems((old) => (reset || !old ? page : [...old, ...page]));
        setMore(page.length > 0);
        setError('');
      })
      .catch((e) => setError(e.message));
  useEffect(() => {
    setItems(null);
    void load(0, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  const loadMore = () => items?.length && load(at(items[items.length - 1]), false);
  return { items, more, error, loadMore };
}

function SearchField({ value, set, label }: { value: string; set: (v: string) => void; label: string }) {
  return (
    <label className="search-field desktop-search">
      <Search size={15} />
      <input
        type="search"
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(e) => set(e.target.value)}
      />
    </label>
  );
}

/** Instances you joined and who was there, from VRChat's own logs. */
export function GameHistory() {
  useLocale();
  const [q, setQ] = useState('');
  const [folder, setFolder] = useState('');
  const { items, more, error, loadMore } = usePages<Visit>(
    (before) => '/desktop/history?before=' + before + '&q=' + encodeURIComponent(q),
    (data) => {
      setFolder(data.folder);
      return data.visits;
    },
    (v) => v.joined,
    [q],
  );
  return (
    <SectionPage
      title="History"
      subtitle="Instances you joined and the people you met, read from VRChat’s logs on this computer."
    >
      <SearchField value={q} set={setQ} label={tr('Search worlds and people')} />
      {error && <div className="error-banner">{error}</div>}
      {items && items.length === 0 && (
        <div className="empty-state">
          <h3>{tr(q ? 'Nothing matches your search.' : 'No history yet')}</h3>
          {!q && (
            <p>
              {tr('VRCove reads VRChat’s logs while it runs. Play VRChat and your instances show up here.')}
              <br />
              <small>{folder}</small>
            </p>
          )}
        </div>
      )}
      <ol className="history-list">
        {items?.map((v) => (
          <li key={v.id} className="dashboard-panel history-visit">
            <div className="history-visit-head">
              <strong>
                <World id={v.worldId} name={v.worldName} />
              </strong>
              <span>
                {when(v.joined)}
                {v.left ? ' – ' + clock(v.left) : ' · ' + tr('now')}
              </span>
            </div>
            {v.people.length > 0 && (
              <ul className="history-people">
                {v.people.map((p) => (
                  <li key={p.displayName + p.joined}>
                    <Person p={p} />
                    {p.avatar && <small>{p.avatar}</small>}
                  </li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ol>
      {more && (
        <button className="standard-button load-more" onClick={() => void loadMore()}>
          {tr('Load more')}
        </button>
      )}
    </SectionPage>
  );
}

const feedKinds = [
  ['', 'Everything'],
  ['location', 'Locations'],
  ['online', 'Came online'],
  ['offline', 'Went offline'],
  ['status', 'Status changes'],
] as const;

function FeedText({ e }: { e: FeedEntry }) {
  switch (e.kind) {
    case 'online':
      return <>{tr('came online')}</>;
    case 'offline':
      return <>{tr('went offline')}</>;
    case 'location':
      return (
        <>
          {tr('joined')}{' '}
          <World id={e.detail?.startsWith('wrld_') ? e.detail.split(':')[0] : undefined} name={e.worldName} />
        </>
      );
    case 'status':
      return (
        <>
          {tr('changed status to')} <em>{e.detail || tr('none')}</em>
        </>
      );
    case 'added':
      return <>{tr('became your friend')}</>;
    case 'removed':
      return <>{tr('is no longer your friend')}</>;
  }
  return null;
}

/** Friends' activity over time, recorded from VRChat's live feed while VRCove runs. */
export function FriendFeed() {
  useLocale();
  const [q, setQ] = useState('');
  const [kind, setKind] = useState('');
  const { items, more, error, loadMore } = usePages<FeedEntry>(
    (before) => '/desktop/feed?before=' + before + '&kind=' + kind + '&q=' + encodeURIComponent(q),
    (data) => data.entries,
    (e) => e.at,
    [q, kind],
  );
  return (
    <SectionPage title="Feed" subtitle="Your friends’ activity, recorded on this computer while VRCove runs.">
      <div className="desktop-filters">
        <SearchField value={q} set={setQ} label={tr('Search friends and worlds')} />
        <select aria-label={tr('Show')} value={kind} onChange={(e) => setKind(e.target.value)}>
          {feedKinds.map(([value, label]) => (
            <option key={value} value={value}>
              {tr(label)}
            </option>
          ))}
        </select>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {items && items.length === 0 && (
        <div className="empty-state">
          <h3>{tr(q || kind ? 'Nothing matches your search.' : 'No activity yet')}</h3>
          {!q && !kind && (
            <p>
              {tr(
                'While you’re signed in to VRChat, friends going online, moving and changing status show up here.',
              )}
            </p>
          )}
        </div>
      )}
      <ol className="feed-list">
        {items?.map((e) => (
          <li key={e.id}>
            <time>{when(e.at)}</time>
            <span>
              <Person p={e} /> <FeedText e={e} />
            </span>
          </li>
        ))}
      </ol>
      {more && (
        <button className="standard-button load-more" onClick={() => void loadMore()}>
          {tr('Load more')}
        </button>
      )}
    </SectionPage>
  );
}

const shotURL = (file: string) => '/api/desktop/screenshot?file=' + encodeURIComponent(file);

/** VRChat screenshots with the world and the people who were there. */
export function Screenshots() {
  useLocale();
  const [month, setMonth] = useState('');
  const [data, setData] = useState<{
    folder: string;
    months: string[];
    month: string;
    screenshots: Shot[];
  } | null>(null);
  const [error, setError] = useState('');
  const [open, setOpen] = useState<Shot | null>(null);
  useEffect(() => {
    api('/desktop/screenshots?month=' + encodeURIComponent(month))
      .then(setData)
      .catch((e) => setError(e.message));
  }, [month]);
  return (
    <SectionPage
      title="Screenshots"
      subtitle="Your VRChat pictures, with the world and the people who were there."
    >
      {data && data.months.length > 0 && (
        <div className="desktop-filters">
          <select aria-label={tr('Month')} value={data.month} onChange={(e) => setMonth(e.target.value)}>
            {data.months.map((m) => (
              <option key={m} value={m}>
                {formatDate(m + '-15T12:00:00', { year: 'numeric', month: 'long' })}
              </option>
            ))}
          </select>
        </div>
      )}
      {error && <div className="error-banner">{error}</div>}
      {data && data.screenshots.length === 0 && (
        <div className="empty-state">
          <h3>{tr('No screenshots found')}</h3>
          <p>
            <small>{data.folder}</small>
          </p>
        </div>
      )}
      <div className="screenshot-grid">
        {data?.screenshots.map((s) => (
          <button key={s.file} className="screenshot-card" onClick={() => setOpen(s)}>
            <img src={shotURL(s.file)} alt="" loading="lazy" decoding="async" />
            <span>
              <strong>{s.worldName || tr('Unknown world')}</strong>
              <small>
                {when(s.taken)}
                {s.people.length > 0 && ' · ' + tr('{count} people', { count: s.people.length })}
              </small>
            </span>
          </button>
        ))}
      </div>
      {open && (
        <Modal title={open.worldName || tr('Screenshot')} close={() => setOpen(null)} wide>
          <div className="screenshot-view">
            <img src={shotURL(open.file)} alt="" />
            <p>
              <World id={open.worldId} name={open.worldName} /> · {when(open.taken)}
            </p>
            {open.people.length > 0 && (
              <ul className="history-people">
                {open.people.map((p) => (
                  <li key={p.displayName}>
                    <Person p={p} />
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Modal>
      )}
    </SectionPage>
  );
}
