import { t as tr, useLocale } from '../lib/i18n';
import { useState, type ReactNode } from 'react';
import { ChevronLeft, ChevronRight, LoaderCircle } from 'lucide-react';

export function Section({ title, state, empty, children }: any) {
  useLocale();
  if (state.error)
    return (
      <section className="entity-section">
        <h3>{title}</h3>
        <p className="fineprint">{state.error}</p>
      </section>
    );
  if (!state.data)
    return (
      <section className="entity-section">
        <h3>{title}</h3>
        <div className="loading-inline">
          <LoaderCircle size={14} className="spin" />
          {tr('Loading…')}
        </div>
      </section>
    );
  if (!state.data.length)
    return empty ? (
      <section className="entity-section">
        <h3>{title}</h3>
        <p className="fineprint">{empty}</p>
      </section>
    ) : null;
  return (
    <section className="entity-section">
      <h3>
        {title}
        <small>{state.data.length}</small>
      </h3>
      {children}
    </section>
  );
}

/* A card grid shown a page at a time, with previous/next and a button for everything, so long lists don't bury the reviews below. */
export function ShowMore<T>({
  items,
  more,
  limit = 8,
  children,
}: {
  items: T[];
  more: string;
  limit?: number;
  children: (item: T) => ReactNode;
}) {
  useLocale();
  const [all, setAll] = useState(false),
    [start, setStart] = useState(0);
  const paged = items.length > limit;
  const shown = all || !paged ? items : items.slice(start, start + limit);
  return (
    <>
      <div className="entity-grid">{shown.map(children)}</div>
      {paged && (
        <div className="pagination show-more">
          <span>
            {all
              ? tr('All {total}', { total: items.length })
              : tr('{from}–{to} of {total}', {
                  from: start + 1,
                  to: Math.min(start + limit, items.length),
                  total: items.length,
                })}
          </span>
          <div>
            {!all && (
              <>
                <button disabled={start === 0} onClick={() => setStart(Math.max(0, start - limit))}>
                  <ChevronLeft size={15} />
                  {tr('Previous')}
                </button>
                <button disabled={start + limit >= items.length} onClick={() => setStart(start + limit)}>
                  {tr('Next')}
                  <ChevronRight size={15} />
                </button>
              </>
            )}
            <button aria-expanded={all} onClick={() => setAll(!all)}>
              {all ? tr('Show fewer') : more}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
