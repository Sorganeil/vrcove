import { useState } from 'react';
import { Search } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useLoader } from '../lib/use-loader';
import Loading from '../components/loading';
import WorldGrid from '../components/world-grid';
import { ErrorBanner, RequireVRChat } from '../components/section-page';
import SearchBar, { SortMenu, type SearchInput } from '../components/search-bar';
import type { WorldSummary } from '../lib/types';

const PAGE_SIZE = 50;

const sorts = [
  ['relevance', 'Best match'],
  ['popular', 'Popular'],
  ['trending', 'Trending'],
  ['favorites', 'Most favorited'],
  ['newest', 'Newest'],
  ['updated', 'Recently updated'],
] as const;

const platforms = [
  ['all', 'All worlds'],
  ['pc', 'PC'],
  ['quest', 'Quest'],
] as const;

/** The Worlds tab: public worlds from VRChat, browsed or searched. */
export default function WorldResults(search: SearchInput) {
  return (
    <RequireVRChat section="Worlds">
      <WorldList {...search} />
    </RequireVRChat>
  );
}

function WorldList({ input, setInput, query, submit }: SearchInput) {
  useLocale();
  // null follows the search: Best match while searching, else Popular.
  const [chosen, setChosen] = useState<string | null>(null);
  const [platform, setPlatform] = useState('all');
  const picked = chosen ?? (query ? 'relevance' : 'popular');
  const sort = picked === 'relevance' && !query ? 'popular' : picked;
  // The offset belongs to one search, so a new search or sort starts at the first page.
  const key = [query, sort, platform].join('\n');
  const [paging, setPaging] = useState({ key, offset: 0 });
  const offset = paging.key === key ? paging.offset : 0;
  const { data, loading, error, retry } = useLoader(
    () => {
      const params: Record<string, string> = { type: 'worlds', q: query, offset: String(offset) };
      // Without a sort, VRChat orders a search by best match.
      if (sort !== 'relevance') params.sort = sort;
      if (platform !== 'all') params.platform = platform;
      return api('/account/search?' + new URLSearchParams(params));
    },
    [query, sort, platform, offset],
    [] as WorldSummary[],
  );
  return (
    <>
      <SearchBar
        label="Search worlds"
        placeholder="Search worlds…"
        value={input}
        onChange={setInput}
        onSubmit={submit}
      />
      <div className="browse-bar">
        <div className="chips" role="group" aria-label={tr('Platform')}>
          {platforms.map(([value, label]) => (
            <button
              key={value}
              className={platform === value ? 'chosen' : ''}
              aria-pressed={platform === value}
              onClick={() => setPlatform(value)}
            >
              {tr(label)}
            </button>
          ))}
        </div>
        <SortMenu
          label="Sort worlds"
          value={sort}
          options={sorts.map(([value, text]) => [value, text, value === 'relevance' && !query] as const)}
          onChange={setChosen}
        />
      </div>
      <ErrorBanner error={error} retry={retry} />
      {loading ? <Loading text={tr('Exploring worlds…')} /> : <WorldGrid worlds={data} />}
      {!loading && !data.length && !error && (
        <div className="empty-state">
          <Search size={30} />
          <h3>{tr('No worlds found')}</h3>
          <p>{tr('Try a different search.')}</p>
        </div>
      )}
      {(data.length > 0 || offset > 0) && (
        <div className="pagination">
          <span>
            {tr('Results')} {data.length ? offset + 1 : 0}–{offset + data.length}
          </span>
          <div>
            <button
              disabled={!offset || loading}
              onClick={() => setPaging({ key, offset: Math.max(0, offset - PAGE_SIZE) })}
            >
              {tr('Previous')}
            </button>
            <button
              disabled={data.length < PAGE_SIZE || loading}
              onClick={() => setPaging({ key, offset: offset + PAGE_SIZE })}
            >
              {tr('Next')}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
