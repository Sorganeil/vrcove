import { useEffect, useState } from 'react';
import { Search, X } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useLoader } from '../lib/use-loader';
import Loading from '../components/loading';
import WorldGrid from '../components/world-grid';
import { ErrorBanner, RequireVRChat, SectionPage } from '../components/section-page';
import type { WorldSummary } from '../lib/types';

/** Popular public worlds, or VRChat world search results. */
export default function Worlds() {
  return (
    <SectionPage title="Worlds" subtitle="Popular public worlds, live from VRChat.">
      <RequireVRChat section="Worlds">
        <WorldList />
      </RequireVRChat>
    </SectionPage>
  );
}

function WorldList() {
  useLocale();
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [offset, setOffset] = useState(0);
  // Typed searches apply after a short pause.
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(search.trim());
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  const { data, loading, error, retry } = useLoader(
    () =>
      api(
        query
          ? '/account/search?' + new URLSearchParams({ type: 'worlds', q: query, offset: String(offset) })
          : '/account/worlds',
      ),
    [query, offset],
    [] as WorldSummary[],
  );
  return (
    <>
      <ErrorBanner error={error} retry={retry} />
      <div className="search-row">
        <div className="search-input">
          <Search size={19} />
          <input
            type="search"
            aria-label={tr('Search worlds')}
            placeholder={tr('Search worlds…')}
            value={search}
            maxLength={100}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search && (
            <button
              aria-label={tr('Clear search')}
              onClick={() => {
                setSearch('');
                setQuery('');
                setOffset(0);
              }}
            >
              <X size={16} />
            </button>
          )}
        </div>
      </div>
      {loading ? <Loading text={tr('Exploring worlds…')} /> : <WorldGrid worlds={data} />}
      {!loading && !data.length && !error && (
        <div className="empty-state">
          <Search size={30} />
          <h3>{tr('No worlds found')}</h3>
          <p>{tr('Try a different search.')}</p>
        </div>
      )}
      {query && (
        <div className="pagination">
          <span>
            {tr('Results')} {data.length ? offset + 1 : 0}–{offset + data.length}
          </span>
          <div>
            <button disabled={!offset || loading} onClick={() => setOffset(Math.max(0, offset - 50))}>
              {tr('Previous')}
            </button>
            <button disabled={data.length < 50 || loading} onClick={() => setOffset(offset + 50)}>
              {tr('Next')}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
