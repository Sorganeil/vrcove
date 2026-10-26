import { siteOrigin } from '../lib/platform';
import { covePath, appLink, goTo } from '../lib/routes';
import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { LoaderCircle, Library, Lock, Globe2, Trash2, Copy, Pencil, X } from 'lucide-react';
import { api } from '../lib/api';
import { day } from '../lib/dates';
import { CollectionForm } from '../components/collection-form';
import { Back, type CollectionInput, Thumb, targetLink } from '../components/cove-shared';
import { useResource } from '../lib/use-resource';
import type { CollectionItem } from '../lib/types';

/* /collection/<id>: one collection, for its owner or anyone if public. */
export function CollectionPage({ id, notify }: { id: string; notify: (s: string) => void }) {
  useLocale();
  const col = useResource('/cove/collection?id=' + encodeURIComponent(id));
  const [editing, setEditing] = useState(false),
    [confirmDelete, setConfirmDelete] = useState(false),
    [busy, setBusy] = useState(''),
    [error, setError] = useState('');
  const c = col.data;
  if (col.error)
    return (
      <div className="entity-page">
        <Back />
        <div className="empty-state">
          <Lock size={30} />
          <h3>{tr('Collection unavailable')}</h3>
          <p>{col.error}</p>
        </div>
      </div>
    );
  if (!c)
    return (
      <div className="loading-state">
        <LoaderCircle className="spin" />
        {tr('Loading collection…')}
      </div>
    );
  async function run(name: string, fn: () => Promise<void>) {
    setBusy(name);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  const save = (v: CollectionInput) =>
    run('save', async () => {
      await api('/cove/collections/update', {
        id,
        name: v.name,
        description: v.description,
        public: v.public,
      });
      setEditing(false);
      col.reload();
      notify(tr('Collection saved'));
    });
  const remove = (target: string) =>
    run('remove:' + target, async () => {
      await api('/cove/collections/remove', { id, target });
      col.reload();
    });
  const destroy = () =>
    run('delete', async () => {
      await api('/cove/collections/delete', { id });
      notify(tr('Collection deleted'));
      goTo('/collections');
    });
  const copy = () =>
    navigator.clipboard
      .writeText(siteOrigin + '/collection/' + id)
      .then(() => notify(tr('Link copied')))
      .catch(() => notify(tr('Clipboard access is unavailable.')));
  return (
    <div className="entity-page">
      <Back />
      {editing ? (
        <CollectionForm
          initial={c}
          kindLocked
          busy={busy === 'save'}
          submit={save}
          cancel={() => setEditing(false)}
        />
      ) : (
        <div className="collection-head">
          <div className="eyebrow">
            {c.public ? <Globe2 size={13} /> : <Lock size={13} />}
            {c.kind === 'avatar' ? 'AVATAR' : tr('WORLD')} {tr('COLLECTION')}
            {c.public ? '' : ' / PRIVATE'}
          </div>
          <h1>{c.name}</h1>
          <p className="creator">
            {c.owner ? (
              <>
                {tr('by')} <a {...appLink(covePath('cove', c.owner))}>{c.owner}</a> ·{' '}
              </>
            ) : c.mine ? (
              ''
            ) : (
              'by a VRCove user · '
            )}
            {c.items.length} {c.kind}
            {c.items.length === 1 ? '' : 's'} {tr('· updated')} {day(c.updated)}
          </p>
          {c.description && <p className="description cove-bio">{c.description}</p>}
          <div className="result-actions">
            {c.public && (
              <button className="standard-button" onClick={copy}>
                <Copy size={13} />
                {tr('Copy link')}
              </button>
            )}
            {c.mine && (
              <>
                <button className="standard-button" onClick={() => setEditing(true)}>
                  <Pencil size={13} />
                  {tr('Edit')}
                </button>
                {confirmDelete ? (
                  <>
                    <button className="standard-button danger-text" disabled={!!busy} onClick={destroy}>
                      {busy === 'delete' ? <LoaderCircle size={13} className="spin" /> : <Trash2 size={13} />}
                      {tr('Delete for good')}
                    </button>
                    <button className="standard-button" onClick={() => setConfirmDelete(false)}>
                      {tr('Keep it')}
                    </button>
                  </>
                ) : (
                  <button className="standard-button danger-text" onClick={() => setConfirmDelete(true)}>
                    <Trash2 size={13} />
                    {tr('Delete')}
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      )}
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      {c.items.length ? (
        <div className={'entity-grid collection-items ' + c.kind}>
          {c.items.map((it: CollectionItem) => (
            <div className="collection-item" key={it.id}>
              <a className="entity-card" {...targetLink(c.kind, it.id)}>
                <div className={'entity-card-image' + (c.kind === 'world' ? ' wide' : ' tall')}>
                  <Thumb url={it.image} alt="" />
                </div>
                <strong>{it.name || it.id}</strong>
                <small>
                  {tr('Added')} {day(it.added)}
                </small>
              </a>
              {c.mine && (
                <button
                  className="icon-button collection-remove"
                  aria-label={'Remove ' + (it.name || it.id)}
                  disabled={!!busy}
                  onClick={() => remove(it.id)}
                >
                  {busy === 'remove:' + it.id ? <LoaderCircle size={14} className="spin" /> : <X size={14} />}
                </button>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div className="empty-state">
          <Library size={30} />
          <h3>{tr('Nothing here yet')}</h3>
          <p>
            {c.mine
              ? 'Use “Add to collection” on any ' + c.kind + ' to add it.'
              : tr('This collection is empty.')}
          </p>
        </div>
      )}
    </div>
  );
}
