import { useEffect, useState } from 'react';
import { ChevronLeft, ChevronRight, Search, SlidersHorizontal, X } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import type { Avatar } from '../lib/avatar';
import AvatarGrid from '../components/avatar-grid';
import { ErrorBanner } from '../components/section-page';
import CompanionDownload from '../components/companion-download';
import SearchBar, { SortMenu, type SearchInput } from '../components/search-bar';
import { replaceSearchParams } from '../lib/search-params';

const PAGE_SIZE = 36;

type Filters = {
  platform: string;
  rating: string;
  /** null follows the search: Best match while searching, else Recently updated. */
  sort: string | null;
  author: { name: string; id?: string } | null;
  tag: string;
  hide: string[];
  fallbackOnly: boolean;
};
const defaults: Filters = {
  platform: 'all',
  rating: 'all',
  sort: null,
  author: null,
  tag: '',
  hide: [],
  fallbackOnly: false,
};

const sorts = [
  ['relevance', 'Best match'],
  ['newest', 'Recently updated'],
  ['added', 'Recently added'],
  ['oldest', 'Oldest updates'],
  ['name', 'Name A–Z'],
  ['name-desc', 'Name Z–A'],
  ['author', 'Creator A–Z'],
  ['rating', 'Best performance'],
] as const;

// The last filters, restored when returning to avatar search during the visit.
let remembered: Filters = defaults;

/** Filters linked from elsewhere (/avatars?author=…&tag=…) replace the remembered search. */
function initialFilters(): Filters {
  const params = new URLSearchParams(window.location.search);
  const author = params.get('author'),
    tag = params.get('tag');
  if (!author && !tag) return remembered;
  return {
    ...defaults,
    author: author ? { name: author, id: params.get('authorId') || undefined } : null,
    tag: tag || '',
  };
}

/** Keeps creator and tag filters in the address bar, so a filtered search can be shared. */
function syncUrl(f: Filters) {
  replaceSearchParams({
    author: f.author?.name || null,
    authorId: f.author?.id || null,
    tag: f.tag || null,
  });
}

/** The Avatars tab: the public avatar catalog, browsed or searched. */
export default function AvatarResults({ input, setInput, query, submit }: SearchInput) {
  useLocale();
  const [filters, setFilters] = useState(initialFilters);
  const [authorInput, setAuthorInput] = useState(filters.author?.name || '');
  const [tagInput, setTagInput] = useState(filters.tag);
  // The page belongs to one search, so a new search starts on page 1.
  const [paging, setPaging] = useState({ query, page: 0 });
  const page = paging.query === query ? paging.page : 0;
  const setPage = (next: number) => setPaging({ query, page: next });
  const [panel, setPanel] = useState(false);
  const [avatars, setAvatars] = useState<Avatar[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const { platform, rating, author, tag, hide, fallbackOnly } = filters;
  // Best match needs a search; without one the newest come first.
  const chosen = filters.sort ?? (query ? 'relevance' : 'newest');
  const sort = chosen === 'relevance' && !query ? 'newest' : chosen;

  function update(patch: Partial<Filters>) {
    setFilters((f) => ({ ...f, ...patch }));
    setPage(0);
  }

  // Creator and tag filters apply after a short pause.
  useEffect(() => {
    const t = setTimeout(() => {
      const name = authorInput.trim();
      setFilters((f) => {
        const nextAuthor = name ? (f.author && f.author.name === name ? f.author : { name }) : null;
        if (f.author === nextAuthor && f.tag === tagInput.trim()) return f;
        return { ...f, author: nextAuthor, tag: tagInput.trim() };
      });
      setPage(0);
    }, 300);
    return () => clearTimeout(t);
    // setPage only depends on the current search, which is fine to read late.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authorInput, tagInput]);

  useEffect(() => {
    remembered = filters;
    syncUrl(filters);
  }, [filters]);

  useEffect(() => {
    let alive = true;
    const params: Record<string, string> = { q: query, platform, rating, sort, page: String(page) };
    if (author) {
      params.author = author.name;
      if (author.id) params.authorId = author.id;
    }
    if (tag) params.tag = tag;
    if (hide.length) params.hide = hide.join(',');
    if (fallbackOnly) params.fallback = '1';
    api('/catalog?' + new URLSearchParams(params))
      .then((d) => {
        if (alive) {
          setAvatars(d.avatars);
          setTotal(d.total);
        }
      })
      .catch((e) => {
        if (alive) setError(e.message);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [query, platform, rating, sort, page, retry, author, tag, hide, fallbackOnly]);

  // Searching again shows the loading state until results arrive.
  const [requested, setRequested] = useState({ filters, query, page, retry });
  if (
    requested.filters !== filters ||
    requested.query !== query ||
    requested.page !== page ||
    requested.retry !== retry
  ) {
    setRequested({ filters, query, page, retry });
    setLoading(true);
    setError('');
  }

  const activeFilters =
    Number(platform !== 'all') +
    Number(rating !== 'all') +
    Number(!!author) +
    Number(!!tag) +
    hide.length +
    Number(fallbackOnly);

  function goToPage(next: number) {
    setPage(next);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  return (
    <>
      <CompanionDownload contribute />
      <SearchBar
        label="Search avatars"
        placeholder="Search avatars, creators, or an avatar ID…"
        value={input}
        onChange={setInput}
        onSubmit={submit}
      >
        <button
          type="button"
          className={'filter-button ' + (panel ? 'chosen' : '')}
          onClick={() => setPanel(!panel)}
          aria-expanded={panel}
        >
          <SlidersHorizontal size={15} />
          {tr('Filters')}
          {activeFilters > 0 && <small>{activeFilters}</small>}
        </button>
      </SearchBar>
      <div className="browse-bar">
        <div className="chips">
          {[
            ['all', 'All avatars'],
            ['both', 'PC + Quest'],
            ['pc', 'PC'],
            ['quest', 'Quest'],
          ].map(([v, l]) => (
            <button
              key={v}
              className={platform === v ? 'chosen' : ''}
              onClick={() => update({ platform: v })}
            >
              {v === 'both' && <i />}
              {l}
            </button>
          ))}
        </div>
        <SortMenu
          label="Sort avatars"
          value={sort}
          options={sorts.map(([value, text]) => [value, text, value === 'relevance' && !query] as const)}
          onChange={(value) => update({ sort: value })}
        />
      </div>
      {panel && (
        <div className="filter-panel">
          <label>
            {tr('PC performance')}
            <select
              aria-label={tr('Performance rating')}
              value={rating}
              onChange={(e) => update({ rating: e.target.value })}
            >
              <option value="all">{tr('Any rating')}</option>
              <option value="Excellent">Excellent</option>
              <option value="Good+">{tr('Good or better')}</option>
              <option value="Medium+">{tr('Medium or better')}</option>
              <option value="Poor+">{tr('Poor or better')}</option>
              <option value="VeryPoor">{tr('Very poor')}</option>
              <option value="Unknown">Unknown</option>
            </select>
          </label>
          <label>
            {tr('Creator')}
            <input
              aria-label={tr('Filter by creator')}
              value={authorInput}
              onChange={(e) => setAuthorInput(e.target.value)}
              placeholder={tr('Exact creator name')}
              maxLength={100}
            />
          </label>
          <label>
            {tr('Tag')}
            <input
              aria-label={tr('Filter by tag')}
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              placeholder={tr('e.g. furry, face tracking')}
              maxLength={60}
            />
          </label>
          <fieldset className="filter-checks">
            <legend>{tr('Content')}</legend>
            {(
              [
                ['adult', 'Hide adult content'],
                ['gore', 'Hide gore and violence'],
                ['horror', 'Hide horror'],
              ] as const
            ).map(([key, label]) => (
              <label key={key}>
                <input
                  type="checkbox"
                  checked={hide.includes(key)}
                  onChange={(e) =>
                    update({ hide: e.target.checked ? [...hide, key] : hide.filter((x) => x !== key) })
                  }
                />
                {tr(label)}
              </label>
            ))}
            <label>
              <input
                type="checkbox"
                checked={fallbackOnly}
                onChange={(e) => update({ fallbackOnly: e.target.checked })}
              />
              {tr('Quest fallback avatars only')}
            </label>
          </fieldset>
          <p>
            {tr('Native builds only. Impostor versions do not count as Quest support.')}{' '}
            {tr('Content filters use the tags creators and VRChat set, so some avatars may be untagged.')}
          </p>
          <button
            onClick={() => {
              setAuthorInput('');
              setTagInput('');
              update({
                platform: 'all',
                rating: 'all',
                author: null,
                tag: '',
                hide: [],
                fallbackOnly: false,
              });
            }}
          >
            {tr('Reset filters')}
          </button>
        </div>
      )}
      {(author || tag) && (
        <div className="active-filters">
          {author && (
            <button
              onClick={() => {
                setAuthorInput('');
                update({ author: null });
              }}
              aria-label={tr('Remove filter') + ': ' + author.name}
            >
              {tr('Creator: {author}', { author: author.name })}
              <X size={13} />
            </button>
          )}
          {tag && (
            <button
              onClick={() => {
                setTagInput('');
                update({ tag: '' });
              }}
              aria-label={tr('Remove filter') + ': ' + tag}
            >
              {tr('Tag: {tag}', { tag })}
              <X size={13} />
            </button>
          )}
        </div>
      )}
      <ErrorBanner error={error} retry={() => setRetry((r) => r + 1)} />
      <div className="results-line">
        <span>{loading ? 'Searching the index…' : tr('{count} avatars', { count: total })}</span>
        <span>{tr('PUBLIC AVATAR CATALOG')}</span>
      </div>
      {loading || avatars.length ? (
        <AvatarGrid avatars={avatars} loading={loading} />
      ) : (
        <div className="empty-state">
          <Search size={30} />
          <h3>{tr('No avatars found')}</h3>
          <p>{tr('Try a different search or loosen your filters.')}</p>
        </div>
      )}
      {total > PAGE_SIZE && (
        <div className="pagination">
          <span>{tr('Page {page} of {pages}', { page: page + 1, pages: Math.ceil(total / PAGE_SIZE) })}</span>
          <div>
            <button disabled={!page || loading} onClick={() => goToPage(page - 1)}>
              <ChevronLeft size={15} />
              {tr('Previous')}
            </button>
            <button disabled={(page + 1) * PAGE_SIZE >= total || loading} onClick={() => goToPage(page + 1)}>
              {tr('Next')}
              <ChevronRight size={15} />
            </button>
          </div>
        </div>
      )}
    </>
  );
}
