import { t as tr, useLocale } from '../lib/i18n';
import { useState, type ReactNode } from 'react';
import { LoaderCircle } from 'lucide-react';

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

/* A card grid that shows the first few items, with a button for the rest, so long lists don't bury the reviews below. */
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
  const [all, setAll] = useState(false);
  return (
    <>
      <div className="entity-grid">{(all ? items : items.slice(0, limit)).map(children)}</div>
      {items.length > limit && (
        <button className="standard-button show-more" aria-expanded={all} onClick={() => setAll(!all)}>
          {all ? tr('Show fewer') : more}
        </button>
      )}
    </>
  );
}
