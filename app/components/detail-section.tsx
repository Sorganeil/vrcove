import { t as tr, useLocale } from '../lib/i18n';
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
