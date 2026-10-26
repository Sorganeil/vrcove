import { useEffect, useState } from 'react';
import { ArrowUpRight, Clock, Globe2, MapPin, Search, UserCheck, UserMinus, Users } from 'lucide-react';
import { api } from '../lib/api';
import { formatDate, t as tr, useLocale } from '../lib/i18n';
import { Picture } from '../components/avatar-media';
import { detailLink } from '../components/detail-links';
import Modal from '../components/modal';
import { Photo } from '../components/photo';
import { SectionPage } from '../components/section-page';
import { regionNames, userThumb } from '../lib/vrchat-format';
import type { VRChatUser } from '../lib/types';

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

type WorldPreview = {
  name?: string;
  imageUrl?: string;
  thumbnailImageUrl?: string;
  authorName?: string;
  loaded?: boolean;
};

type UserPreview = {
  displayName?: string;
  thumbUrl?: string;
  loaded?: boolean;
};

const rawUserIdPattern = /^usr_[0-9a-f-]{36}$/i;
const worldIdPattern = /^wrld_[0-9a-f-]{36}$/i;

const worldCache = new Map<string, WorldPreview>();
const worldInflight = new Map<string, Promise<void>>();
const userCache = new Map<string, UserPreview>();
const userInflight = new Map<string, Promise<void>>();
let friendsPrimed = false;

export function isUnresolvedUser(name?: string, userId?: string) {
  const trimmed = (name || '').trim();
  return !trimmed || trimmed === userId || rawUserIdPattern.test(trimmed);
}

function extractWorldId(value?: string): string | undefined {
  if (!value) return undefined;
  const candidate = value.split(':')[0].trim();
  return worldIdPattern.test(candidate) ? candidate : undefined;
}

const when = (ms: number) =>
  formatDate(new Date(ms).toISOString(), {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
const clock = (ms: number) => formatDate(new Date(ms).toISOString(), { hour: 'numeric', minute: '2-digit' });

function formatDuration(joined: number, left?: number): string {
  if (!left || left < joined) return '';
  const minutes = Math.round((left - joined) / 60000);
  if (minutes < 1) return '< 1m';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const rem = minutes % 60;
  return rem > 0 ? `${hours}h ${rem}m` : `${hours}h`;
}

function parseLocationMeta(location?: string): { access?: string; region?: string } {
  if (!location || location === 'private' || location === 'offline' || location === 'traveling') {
    return {};
  }
  const instance = location.includes(':') ? location.slice(location.indexOf(':') + 1) : '';
  if (!instance) return {};
  let access = 'Public';
  if (instance.includes('~group(')) {
    if (instance.includes('~groupAccessType(public)')) access = 'Group Public';
    else if (instance.includes('~groupAccessType(plus)')) access = 'Group+';
    else access = 'Group';
  } else if (instance.includes('~private(')) {
    access = instance.includes('~canRequestInvite') ? 'Invite+' : 'Invite';
  } else if (instance.includes('~friends(')) {
    access = 'Friends';
  } else if (instance.includes('~hidden(')) {
    access = 'Friends+';
  }
  const regionMatch = /~region\(([a-z]+)\)/.exec(instance);
  const regionCode = regionMatch?.[1];
  const region = regionCode ? regionNames[regionCode] || regionCode.toUpperCase() : undefined;
  return { access, region };
}

/** Fetches and caches world banners and titles for visible world IDs. */
function useWorldPreviews(worldIds: string[]) {
  const [, setTick] = useState(0);
  const key = [...new Set(worldIds.filter((id) => worldIdPattern.test(id)))].sort().join(',');
  useEffect(() => {
    if (!key) return;
    let alive = true;
    const ids = key.split(',').filter(Boolean);
    for (const id of ids) {
      if (worldCache.has(id) || worldInflight.has(id)) continue;
      const promise = api('/account/world?id=' + encodeURIComponent(id))
        .then((data) => {
          worldCache.set(id, {
            name: data?.name,
            imageUrl: data?.imageUrl,
            thumbnailImageUrl: data?.thumbnailImageUrl,
            authorName: data?.authorName,
            loaded: true,
          });
        })
        .catch(() => {
          worldCache.set(id, { loaded: true });
        })
        .finally(() => {
          worldInflight.delete(id);
          if (alive) setTick((n) => n + 1);
        });
      worldInflight.set(id, promise);
    }
    return () => {
      alive = false;
    };
  }, [key]);
  return worldCache;
}

/** Resolves user display names and avatar thumbnails for feed and history items. */
function useUserPreviews(entries: { userId?: string; displayName?: string }[], primeFriends = false) {
  const [, setTick] = useState(0);
  const unresolvedIds = [
    ...new Set(
      entries
        .filter(
          (e) => e.userId && rawUserIdPattern.test(e.userId) && isUnresolvedUser(e.displayName, e.userId),
        )
        .map((e) => e.userId!),
    ),
  ].sort();
  const allIds = [
    ...new Set(entries.map((e) => e.userId).filter((id): id is string => !!id && rawUserIdPattern.test(id))),
  ].sort();
  const key = unresolvedIds.join(',') + '|' + allIds.join(',');

  useEffect(() => {
    let alive = true;
    const bump = () => {
      if (alive) setTick((n) => n + 1);
    };

    const run = async () => {
      if (primeFriends && !friendsPrimed && allIds.length > 0) {
        friendsPrimed = true;
        const lists = await Promise.allSettled([
          api('/account/friends?offline=false'),
          api('/account/friends?offline=true'),
        ]);
        for (const res of lists) {
          if (res.status === 'fulfilled' && Array.isArray(res.value)) {
            for (const f of res.value as VRChatUser[]) {
              if (f?.id) {
                userCache.set(f.id, {
                  displayName: f.displayName,
                  thumbUrl: userThumb(f),
                  loaded: true,
                });
              }
            }
          }
        }
        bump();
      }

      const toFetch = allIds.filter((id) => {
        const cached = userCache.get(id);
        if (userInflight.has(id)) return false;
        if (!cached) return unresolvedIds.includes(id) || primeFriends;
        if (unresolvedIds.includes(id) && isUnresolvedUser(cached.displayName, id) && !cached.loaded)
          return true;
        return false;
      });

      for (const id of toFetch.slice(0, 12)) {
        const promise = api('/account/user?id=' + encodeURIComponent(id))
          .then((u: VRChatUser) => {
            userCache.set(id, {
              displayName: u?.displayName,
              thumbUrl: userThumb(u || {}),
              loaded: true,
            });
          })
          .catch(() => {
            const prev = userCache.get(id);
            userCache.set(id, { ...prev, loaded: true });
          })
          .finally(() => {
            userInflight.delete(id);
            bump();
          });
        userInflight.set(id, promise);
      }
    };

    void run();
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, primeFriends]);

  return userCache;
}

function resolveDisplayName(
  p: { userId?: string; displayName?: string },
  users?: Map<string, UserPreview>,
): string {
  const cached = p.userId ? (users ?? userCache).get(p.userId)?.displayName : undefined;
  if (!isUnresolvedUser(p.displayName, p.userId)) return p.displayName!;
  if (cached && !isUnresolvedUser(cached, p.userId)) return cached;
  return p.displayName || p.userId || tr('Unknown user');
}

function Person({
  p,
  users,
}: {
  p: Pick<Encounter, 'userId' | 'displayName'>;
  users?: Map<string, UserPreview>;
}) {
  const name = resolveDisplayName(p, users);
  return p.userId ? (
    <a className="entity-link user-link" {...detailLink('user', p.userId)}>
      <span>{name}</span>
      <ArrowUpRight size={13} aria-hidden="true" />
    </a>
  ) : (
    <span className="entity-static">{name}</span>
  );
}

function World({ id, name, fallbackName }: { id?: string; name?: string; fallbackName?: string }) {
  const validId = extractWorldId(id);
  const label = (name || fallbackName || '').trim();
  if (!label && !validId) return <span className="entity-static muted">{tr('a private instance')}</span>;
  const display = label || tr('Unknown world');
  return validId ? (
    <a className="entity-link world-link" {...detailLink('world', validId)}>
      <span>{display}</span>
      <ArrowUpRight size={14} aria-hidden="true" />
    </a>
  ) : (
    <span className="entity-static">{display}</span>
  );
}

function HistoryPersonChip({ p, users }: { p: Encounter; users?: Map<string, UserPreview> }) {
  const name = resolveDisplayName(p, users);
  const inner = (
    <>
      <span className="history-person-name">
        <span>{name}</span>
        {p.userId && <ArrowUpRight size={12} aria-hidden="true" />}
      </span>
      {p.avatar && (
        <small className="history-person-avatar" title={p.avatar}>
          {p.avatar}
        </small>
      )}
    </>
  );
  return p.userId ? (
    <a className="history-person is-clickable" {...detailLink('user', p.userId)}>
      {inner}
    </a>
  ) : (
    <div className="history-person">{inner}</div>
  );
}

/** Loads pages of a list that is ordered newest first, `before` a time. */
function usePages<T>(
  path: (before: number) => string,
  key: (data: any) => T[],
  at: (item: T) => number,
  deps: unknown[],
  pageSize = 30,
) {
  const [items, setItems] = useState<T[] | null>(null);
  const [more, setMore] = useState(false);
  const [error, setError] = useState('');
  const load = (before: number, reset: boolean) =>
    api(path(before))
      .then((data) => {
        const page = key(data) || [];
        setItems((old) => (reset || !old ? page : [...old, ...page]));
        setMore(page.length >= pageSize);
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
    30,
  );

  const worldIds = (items || [])
    .map((v) => extractWorldId(v.worldId) || extractWorldId(v.location))
    .filter((id): id is string => !!id);
  const worlds = useWorldPreviews(worldIds);
  const allEncounters = (items || []).flatMap((v) => v.people);
  const users = useUserPreviews(allEncounters, false);

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
        {items?.map((v) => {
          const wId = extractWorldId(v.worldId) || extractWorldId(v.location);
          const worldInfo = wId ? worlds.get(wId) : undefined;
          const worldTitle = v.worldName || worldInfo?.name || tr('Unknown world');
          const bannerSrc = worldInfo?.thumbnailImageUrl || worldInfo?.imageUrl;
          const bannerFallback = worldInfo?.imageUrl;
          const duration = formatDuration(v.joined, v.left);
          const meta = parseLocationMeta(v.location);

          const bannerInner = (
            <>
              {bannerSrc ? (
                <Picture src={bannerSrc} fallbackSrc={bannerFallback} alt={worldTitle} />
              ) : (
                <div className="history-banner-placeholder" aria-hidden="true">
                  <Globe2 size={26} strokeWidth={1.4} />
                  <span>{meta.access || tr('World')}</span>
                </div>
              )}
              {wId && (
                <span className="history-banner-corner" aria-hidden="true">
                  <ArrowUpRight size={13} />
                </span>
              )}
            </>
          );

          return (
            <li key={v.id} className="dashboard-panel history-visit">
              <div className="history-visit-top">
                {wId ? (
                  <a
                    className="history-world-banner is-clickable"
                    aria-label={worldTitle}
                    {...detailLink('world', wId)}
                  >
                    {bannerInner}
                  </a>
                ) : (
                  <div className="history-world-banner">{bannerInner}</div>
                )}
                <div className="history-visit-body">
                  <div className="history-visit-head">
                    <strong className="history-world-heading">
                      <World id={wId} name={v.worldName} fallbackName={worldInfo?.name} />
                    </strong>
                    <span className="history-visit-time">
                      <Clock size={13} aria-hidden="true" />
                      {when(v.joined)}
                      {v.left ? ' – ' + clock(v.left) : ' · ' + tr('now')}
                    </span>
                  </div>
                  <div className="history-visit-badges">
                    {!v.left ? (
                      <span className="history-badge is-active">{tr('Active now')}</span>
                    ) : duration ? (
                      <span className="history-badge">{duration}</span>
                    ) : null}
                    {meta.access && <span className="history-badge">{tr(meta.access)}</span>}
                    {meta.region && <span className="history-badge">{meta.region}</span>}
                    <span className="history-badge">
                      <Users size={12} aria-hidden="true" />
                      {v.people.length} {tr(v.people.length === 1 ? 'person' : 'people')}
                    </span>
                    {worldInfo?.authorName && (
                      <span className="history-world-author">
                        {tr('by')} {worldInfo.authorName}
                      </span>
                    )}
                  </div>
                </div>
              </div>
              {v.people.length > 0 && (
                <ul className="history-people">
                  {v.people.map((p, index) => (
                    <li key={(p.userId || p.displayName) + '-' + p.joined + '-' + index}>
                      <HistoryPersonChip p={p} users={users} />
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}
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

function parseStatusDetail(detail?: string): { tone: string; label: string; message: string } {
  const raw = (detail || '').trim();
  if (!raw) return { tone: 'offline', label: tr('none'), message: '' };
  const prefixes: [string, string, string][] = [
    ['join me', 'join-me', 'Join me'],
    ['ask me', 'ask-me', 'Ask me'],
    ['busy', 'busy', 'Do not disturb'],
    ['active', 'active', 'Online'],
    ['offline', 'offline', 'Offline'],
  ];
  const lower = raw.toLowerCase();
  for (const [prefix, tone, label] of prefixes) {
    if (lower === prefix || lower.startsWith(prefix + ' ')) {
      return {
        tone,
        label: tr(label),
        message: raw.slice(prefix.length).trim(),
      };
    }
  }
  return { tone: 'active', label: '', message: raw };
}

function FeedText({ e, worldInfo }: { e: FeedEntry; worldInfo?: WorldPreview }) {
  const worldId = extractWorldId(e.detail);
  const locMeta = parseLocationMeta(e.detail);
  switch (e.kind) {
    case 'online':
      return (
        <>
          <span className="feed-event-pill is-online">{tr('came online')}</span>
          {(worldId || e.worldName) && (
            <>
              {' '}
              <span className="feed-verb">{tr('in')}</span>{' '}
              <World id={worldId} name={e.worldName} fallbackName={worldInfo?.name} />
            </>
          )}
        </>
      );
    case 'offline':
      return <span className="feed-event-pill is-offline">{tr('went offline')}</span>;
    case 'location':
      return (
        <>
          <span className="feed-verb">{tr('joined')}</span>{' '}
          <World id={worldId} name={e.worldName} fallbackName={worldInfo?.name} />
          {(locMeta.access || locMeta.region) && (
            <span className="feed-instance-meta">
              {[locMeta.access && tr(locMeta.access), locMeta.region].filter(Boolean).join(' · ')}
            </span>
          )}
        </>
      );
    case 'status': {
      const parsed = parseStatusDetail(e.detail);
      return (
        <>
          <span className="feed-verb">{tr('changed status to')}</span>{' '}
          <span className={'feed-status-pill tone-' + parsed.tone}>
            <i className={'presence-dot ' + parsed.tone} aria-hidden="true" />
            {parsed.label && <strong>{parsed.label}</strong>}
            {parsed.message && <em>{parsed.message}</em>}
          </span>
        </>
      );
    }
    case 'added':
      return <span className="feed-event-pill is-online">{tr('became your friend')}</span>;
    case 'removed':
      return <span className="feed-event-pill is-offline">{tr('is no longer your friend')}</span>;
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
    100,
  );

  const worldIds = (items || []).map((e) => extractWorldId(e.detail)).filter((id): id is string => !!id);
  const worlds = useWorldPreviews(worldIds);
  const users = useUserPreviews(items || [], true);

  return (
    <SectionPage title="Feed" subtitle="Your friends’ activity, recorded on this computer while VRCove runs.">
      <div className="desktop-filters">
        <SearchField value={q} set={setQ} label={tr('Search friends and worlds')} />
        <select
          className="desktop-select"
          aria-label={tr('Show')}
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          {feedKinds.map(([value, label]) => (
            <option key={value} value={value}>
              {tr(label)}
            </option>
          ))}
        </select>
      </div>
      <div className="chips feed-chips" role="group" aria-label={tr('Filter activity')}>
        {feedKinds.map(([value, label]) => (
          <button
            key={value}
            type="button"
            className={kind === value ? 'chosen' : ''}
            aria-pressed={kind === value}
            onClick={() => setKind(value)}
          >
            {tr(label)}
          </button>
        ))}
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
        {items?.map((e) => {
          const resolvedName = resolveDisplayName(e, users);
          const userInfo = e.userId ? users.get(e.userId) : undefined;
          const worldId = extractWorldId(e.detail);
          const worldInfo = worldId ? worlds.get(worldId) : undefined;
          const worldBanner = worldInfo?.thumbnailImageUrl || worldInfo?.imageUrl;
          const worldTitle = e.worldName || worldInfo?.name || tr('View world');

          return (
            <li key={e.id} className={'feed-item kind-' + e.kind}>
              {e.userId ? (
                <a
                  className="feed-avatar is-clickable"
                  aria-label={resolvedName}
                  {...detailLink('user', e.userId)}
                >
                  <Photo url={userInfo?.thumbUrl} name={resolvedName} alt="" />
                  <span className={'feed-kind-dot kind-' + e.kind} aria-hidden="true">
                    {e.kind === 'location' ? (
                      <MapPin size={10} />
                    ) : e.kind === 'added' ? (
                      <UserCheck size={10} />
                    ) : e.kind === 'removed' ? (
                      <UserMinus size={10} />
                    ) : null}
                  </span>
                </a>
              ) : (
                <div className="feed-avatar">
                  <Photo url={userInfo?.thumbUrl} name={resolvedName} alt="" />
                </div>
              )}
              <div className="feed-main">
                <div className="feed-statement">
                  <Person p={{ userId: e.userId, displayName: resolvedName }} users={users} />{' '}
                  <FeedText e={e} worldInfo={worldInfo} />
                </div>
                <time className="feed-time">{when(e.at)}</time>
              </div>
              {worldId && (
                <a
                  className="feed-world-thumb is-clickable"
                  aria-label={worldTitle}
                  title={worldTitle}
                  {...detailLink('world', worldId)}
                >
                  {worldBanner ? (
                    <Picture src={worldBanner} fallbackSrc={worldInfo?.imageUrl} alt={worldTitle} />
                  ) : (
                    <div className="feed-world-fallback" aria-hidden="true">
                      <Globe2 size={16} />
                    </div>
                  )}
                </a>
              )}
            </li>
          );
        })}
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
          <select
            className="desktop-select"
            aria-label={tr('Month')}
            value={data.month}
            onChange={(e) => setMonth(e.target.value)}
          >
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
                    <HistoryPersonChip p={p} />
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
