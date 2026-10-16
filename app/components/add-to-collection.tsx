import { appLink } from '../lib/routes';
import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect, useRef } from 'react';
import { LoaderCircle, Library, Plus, Check, Users, X, FolderPlus } from 'lucide-react';
import { api } from '../lib/api';
import { CollectionKind, Summary } from './cove-shared';
import { useResource } from '../lib/use-resource';

/* Button plus dialog that toggles an avatar or world in the user's collections. */
export function AddToCollection({
  kind,
  target,
  name,
  image,
  className = 'standard-button',
}: {
  kind: CollectionKind;
  target: string;
  name: string;
  image?: string;
  className?: string;
}) {
  useLocale();
  const [open, setOpen] = useState(false);
  return (
    <>
      {
        <button type="button" className={className} onClick={() => setOpen(true)}>
          <FolderPlus size={16} />
          {tr('Add to collection')}
        </button>
      }
      {open && (
        <CollectionDialog
          kind={kind}
          target={target}
          name={name}
          image={image}
          close={() => setOpen(false)}
        />
      )}
    </>
  );
}

export function CollectionDialog({
  kind,
  target,
  name,
  image,
  close,
}: {
  kind: CollectionKind;
  target: string;
  name: string;
  image?: string;
  close: () => void;
}) {
  useLocale();
  const list = useResource('/cove/collections?kind=' + kind + '&contains=' + encodeURIComponent(target));
  const [busy, setBusy] = useState(''),
    [error, setError] = useState(''),
    [newName, setNewName] = useState(''),
    [changed, setChanged] = useState<Record<string, boolean>>({}),
    [status, setStatus] = useState('');
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement,
      old = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    ref.current?.focus();
    return () => {
      document.body.style.overflow = old;
      previous?.focus();
    };
  }, []);
  function dialogKey(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      e.nativeEvent.stopImmediatePropagation();
      close();
    }
    if (e.key === 'Tab') {
      const nodes = Array.from(
          ref.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled)') ||
            [],
        ),
        first = nodes[0],
        last = nodes.at(-1);
      if (e.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && (document.activeElement === last || document.activeElement === ref.current)) {
        e.preventDefault();
        first?.focus();
      }
    }
  }
  const item = { target, name: name.slice(0, 120), image: image || '' };
  async function toggle(c: Summary) {
    const has = changed[c.id] ?? c.contains;
    setBusy(c.id);
    setError('');
    setStatus('');
    try {
      await api('/cove/collections/' + (has ? 'remove' : 'add'), { id: c.id, ...item });
      setChanged((m) => ({ ...m, [c.id]: !has }));
      setStatus(tr(has ? 'Removed from collection.' : 'Added to collection.'));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy('new');
    setError('');
    setStatus('');
    try {
      const r = await api('/cove/collections/create', {
        kind,
        name: newName,
        description: '',
        public: false,
      });
      await api('/cove/collections/add', { id: r.id, ...item });
      setNewName('');
      setChanged((m) => ({ ...m, [r.id]: true }));
      setStatus(tr('Added to collection.'));
      list.reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        ref={ref}
        tabIndex={-1}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={tr('Add to collection')}
        onKeyDown={dialogKey}
      >
        <button className="modal-close" aria-label={tr('Close dialog')} onClick={close}>
          <X size={20} />
        </button>
        <div className="login-content">
          <div className="eyebrow">
            <Library size={13} />
            {kind === 'avatar' ? 'AVATAR' : tr('WORLD')} {tr('COLLECTIONS')}
          </div>
          <h2>{tr('Add to collection')}</h2>
          <p>{tr('Choose a collection for {name}.', { name })}</p>
          <p className="fineprint">
            {tr('Select a collection to add this item. Changes are saved immediately.')}
          </p>
          {list.status === 401 || list.status === 403 ? (
            <div className="empty-state">
              <Users size={26} />
              <h3>{tr(list.status === 403 ? 'Link a VRChat account' : 'Sign in to VRCove')}</h3>
              <p>{tr('Sign in to VRCove, then link VRChat to use collections.')}</p>
              <a className="primary" {...appLink('/account', close)}>
                {tr('Sign in')}
              </a>
            </div>
          ) : list.error ? (
            <div className="form-error">{list.error}</div>
          ) : !list.data ? (
            <div className="loading-inline">
              <LoaderCircle size={14} className="spin" />
              {tr('Loading…')}
            </div>
          ) : (
            <>
              {list.data.length > 0 && (
                <ul className="collection-choices">
                  {list.data.map((c: Summary) => {
                    const has = changed[c.id] ?? c.contains;
                    return (
                      <li key={c.id}>
                        <button
                          type="button"
                          role="checkbox"
                          aria-checked={has}
                          disabled={!!busy}
                          onClick={() => toggle(c)}
                        >
                          <span className={'check' + (has ? ' on' : '')}>
                            {busy === c.id ? (
                              <LoaderCircle size={13} className="spin" />
                            ) : (
                              has && <Check size={13} />
                            )}
                          </span>
                          <span>
                            <strong>{c.name}</strong>
                            <small>
                              {c.count +
                                (changed[c.id] !== undefined && changed[c.id] !== c.contains
                                  ? has
                                    ? 1
                                    : -1
                                  : 0)}{' '}
                              {tr('items')}
                              {c.public ? '' : ' · private'}
                            </small>
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
              <p className="fineprint">{tr('Or create a new collection (optional)')}</p>
              <form className="collection-quick" onSubmit={create}>
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  maxLength={80}
                  required
                  placeholder={tr('New collection')}
                  aria-label={tr('New collection name')}
                />
                <button className="standard-button" type="submit" disabled={!!busy || !newName.trim()}>
                  {busy === 'new' ? <LoaderCircle size={14} className="spin" /> : <Plus size={14} />}
                  {tr('Create & add')}
                </button>
              </form>
              <p className="fineprint">
                {tr('New collections start private.')}{' '}
                <a className="text-link" {...appLink('/collections', close)}>
                  {tr('Manage collections')}
                </a>
              </p>
            </>
          )}
          {error && (
            <div className="form-error" role="alert">
              {error}
            </div>
          )}
          {status && (
            <p className="collection-save-status" role="status">
              <Check size={15} />
              {status}
            </p>
          )}
          <div className="result-actions">
            <button type="button" className="primary" disabled={!!busy} onClick={close}>
              {tr('Done')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
