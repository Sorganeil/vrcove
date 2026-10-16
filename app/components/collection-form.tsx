import { t as tr, useLocale } from '../lib/i18n';
import { useState } from 'react';
import { LoaderCircle, Check } from 'lucide-react';
import { CollectionKind, Summary } from './cove-shared';
import type { CollectionInput } from './cove-shared';

export function CollectionForm({
  initial,
  kindLocked,
  busy,
  submit,
  cancel,
}: {
  initial?: Partial<Summary>;
  kindLocked?: boolean;
  busy: boolean;
  submit: (v: CollectionInput) => void;
  cancel?: () => void;
}) {
  useLocale();
  const [kind, setKind] = useState<CollectionKind>(initial?.kind || 'avatar'),
    [name, setName] = useState(initial?.name || ''),
    [description, setDescription] = useState(initial?.description || ''),
    [pub, setPub] = useState(!!initial?.public);
  return (
    <form
      className="cove-panel login-content collection-form"
      onSubmit={(e) => {
        e.preventDefault();
        submit({ kind, name, description, public: pub });
      }}
    >
      {!kindLocked && (
        <div className="chips" role="radiogroup" aria-label={tr('Collection type')}>
          <button
            type="button"
            role="radio"
            aria-checked={kind === 'avatar'}
            className={kind === 'avatar' ? 'chosen' : ''}
            onClick={() => setKind('avatar')}
          >
            {tr('Avatars')}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={kind === 'world'}
            className={kind === 'world' ? 'chosen' : ''}
            onClick={() => setKind('world')}
          >
            {tr('Worlds')}
          </button>
        </div>
      )}
      <label>
        {tr('Name')}
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          maxLength={80}
          placeholder={
            kind === 'avatar' ? 'e.g. Quest-friendly favourites' : 'e.g. Chill worlds for late nights'
          }
        />
      </label>
      <label>
        {tr('Description')}
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          maxLength={1000}
          rows={3}
          placeholder={tr('Optional')}
        />
      </label>
      <label className="cove-switch">
        <input type="checkbox" checked={pub} onChange={(e) => setPub(e.target.checked)} />
        <span>
          {tr('Public: anyone with the link can view it')}{' '}
          {tr('and it’s listed on your profile if your profile is public')}
        </span>
      </label>
      <div className="result-actions">
        <button className="primary" type="submit" disabled={busy}>
          {busy ? <LoaderCircle size={14} className="spin" /> : <Check size={14} />}
          {initial?.id ? tr('Save') : 'Create collection'}
        </button>
        {cancel && (
          <button type="button" className="standard-button" onClick={cancel}>
            {tr('Cancel')}
          </button>
        )}
      </div>
    </form>
  );
}
