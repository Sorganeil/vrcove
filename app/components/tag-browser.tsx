import { useEffect, useMemo, useState } from 'react';
import { Search } from 'lucide-react';
import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import Modal from './modal';

export type CatalogTag = { tag: string; count: number };

// One request serves the popular row and the tag browser for the whole visit.
let loading: Promise<CatalogTag[]> | null = null;

function loadTags(): Promise<CatalogTag[]> {
  loading ??= api('/catalog/tags')
    .then((body: { tags?: CatalogTag[] }) => body.tags || [])
    .catch((error: unknown) => {
      loading = null;
      throw error;
    });
  return loading;
}

/** Creator tags, most used first. Empty until they load or when loading fails. */
export function useCatalogTags() {
  const [tags, setTags] = useState<CatalogTag[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let alive = true;
    loadTags().then(
      (list) => alive && setTags(list),
      () => alive && setFailed(true),
    );
    return () => {
      alive = false;
    };
  }, [retry]);
  return {
    tags,
    failed,
    retry: () => {
      setFailed(false);
      setRetry((r) => r + 1);
    },
  };
}

const PAGE = 120;

/** Searchable list of every creator tag, sorted by use or by name. */
export default function TagBrowser({
  chosen,
  choose,
  close,
}: {
  chosen: string;
  choose: (tag: string) => void;
  close: () => void;
}) {
  useLocale();
  const { tags, failed, retry } = useCatalogTags();
  const [search, setSearch] = useState('');
  const [order, setOrder] = useState<'count' | 'name'>('count');
  const [shown, setShown] = useState(PAGE);
  const matches = useMemo(() => {
    const words = search
      .toLowerCase()
      .split(/[\s_-]+/)
      .filter(Boolean);
    const list = (tags || []).filter((t) => words.every((w) => t.tag.includes(w)));
    return order === 'name' ? [...list].sort((a, b) => a.tag.localeCompare(b.tag)) : list;
  }, [tags, search, order]);

  return (
    <Modal title={tr('All tags')} close={close} wide>
      <div className="tag-browser">
        <h2>{tr('All tags')}</h2>
        <div className="tag-browser-tools">
          <label className="tag-browser-search">
            <Search size={16} aria-hidden />
            <input
              autoFocus
              type="search"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setShown(PAGE);
              }}
              placeholder={tr('Search tags')}
              aria-label={tr('Search tags')}
            />
          </label>
          <div className="chips" role="group" aria-label={tr('Sort tags')}>
            <button
              type="button"
              className={order === 'count' ? 'chosen' : ''}
              aria-pressed={order === 'count'}
              onClick={() => setOrder('count')}
            >
              {tr('Most used')}
            </button>
            <button
              type="button"
              className={order === 'name' ? 'chosen' : ''}
              aria-pressed={order === 'name'}
              onClick={() => setOrder('name')}
            >
              {tr('A–Z')}
            </button>
          </div>
        </div>
        {failed ? (
          <p className="tag-browser-empty">
            {tr('Tags could not be loaded.')}{' '}
            <button type="button" className="link" onClick={retry}>
              {tr('Try again')}
            </button>
          </p>
        ) : !tags ? (
          <p className="tag-browser-empty">{tr('Loading tags…')}</p>
        ) : matches.length === 0 ? (
          <p className="tag-browser-empty">{tr('No tags match your search.')}</p>
        ) : (
          <>
            <p className="tag-browser-count">{tr('{count} tags', { count: matches.length })}</p>
            <ul className="tag-browser-list">
              {matches.slice(0, shown).map(({ tag, count }) => (
                <li key={tag}>
                  <button
                    type="button"
                    className={chosen === tag ? 'chosen' : ''}
                    aria-pressed={chosen === tag}
                    onClick={() => {
                      choose(tag);
                      close();
                    }}
                  >
                    <span>{tag}</span>
                    <small>{tr('{count} avatars', { count })}</small>
                  </button>
                </li>
              ))}
            </ul>
            {shown < matches.length && (
              <button type="button" className="tag-browser-more" onClick={() => setShown((n) => n + PAGE)}>
                {tr('Show more tags')}
              </button>
            )}
          </>
        )}
        <p className="tag-browser-note">
          {tr('Tags come from creators and may be incomplete.')}{' '}
          {tr('Counts leave out adult avatars and update every few hours.')}
        </p>
      </div>
    </Modal>
  );
}
