import { covePath, appLink, goTo } from '../lib/routes';
import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { LoaderCircle, Library, Plus } from 'lucide-react';
import { api } from '../lib/api';
import { CollectionForm } from '../components/collection-form';
import { CollectionCard, type CollectionInput, Summary } from '../components/cove-shared';
import { useResource } from '../lib/use-resource';

/* #collections: the signed-in account's own collections. */
export function CollectionsPage({ notify }: { notify: (s: string) => void }) {
  useLocale();
  const list = useResource('/cove/collections');
  const [creating, setCreating] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  const heading = (
    <div className="section-heading">
      <div>
        <h2>{tr('My collections')}</h2>
        <p>{tr('Group avatars or worlds into lists. Keep them private or share them by link.')}</p>
      </div>
      {list.data && !creating && (
        <button className="primary" onClick={() => setCreating(true)}>
          <Plus size={15} />
          {tr('New collection')}
        </button>
      )}
    </div>
  );
  async function create(v: CollectionInput) {
    setBusy(true);
    setError('');
    try {
      const r = await api('/cove/collections/create', v);
      notify(tr('Collection created'));
      goTo(covePath('collection', r.id));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (list.status === 401 || list.status === 403 || list.status === 503)
    return (
      <section className="catalog-section">
        {heading}
        <div className="empty-state">
          <Library size={30} />
          <h3>
            {list.status === 401
              ? 'Sign in to make collections'
              : list.status === 403
                ? 'Link a VRChat account'
                : 'Accounts aren’t enabled here'}
          </h3>
          <p>
            {list.status === 401
              ? 'Collections belong to your VRCove account, not your VRChat account.'
              : list.error}
          </p>
          {(list.status === 401 || list.status === 403) && (
            <a className="primary" {...appLink('/account')}>
              {tr('Sign in to VRCove')}
            </a>
          )}
        </div>
      </section>
    );
  return (
    <section className="catalog-section">
      {heading}
      {creating && <CollectionForm busy={busy} submit={create} cancel={() => setCreating(false)} />}
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      {list.error ? (
        <div className="error-banner" role="alert">
          {list.error}
        </div>
      ) : !list.data ? (
        <div className="loading-state">
          <LoaderCircle className="spin" />
          {tr('Loading…')}
        </div>
      ) : list.data.length ? (
        <div className="entity-grid collection-grid">
          {list.data.map((c: Summary) => (
            <CollectionCard key={c.id} c={c} />
          ))}
        </div>
      ) : (
        !creating && (
          <div className="empty-state">
            <Library size={30} />
            <h3>{tr('No collections yet')}</h3>
            <p>{tr('Create one here, or use “Add to collection” on any avatar or world.')}</p>
          </div>
        )
      )}
    </section>
  );
}
