import { api } from '../lib/api';
import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect } from 'react';
import { LoaderCircle, Trash2, Star, Check, ArrowUpRight } from 'lucide-react';
import { covePath, appLink } from '../lib/routes';
import { day } from '../lib/dates';
import type { Review } from '../lib/types';
import { ReportButton } from './moderation';

export function Stars({ value, size = 14 }: { value: number; size?: number }) {
  useLocale();
  return (
    <span
      className="stars"
      aria-label={tr('{count} out of 5 stars', {
        count: value.toLocaleString(document.documentElement.lang, {
          minimumFractionDigits: 1,
          maximumFractionDigits: 1,
        }),
      })}
    >
      {[1, 2, 3, 4, 5].map((n) => (
        <Star key={n} size={size} fill={value >= n - 0.5 ? 'currentColor' : 'none'} />
      ))}
    </span>
  );
}

export function StarInput({
  value,
  onChange,
  disabled,
}: {
  value: number;
  onChange: (n: number) => void;
  disabled?: boolean;
}) {
  useLocale();
  return (
    <div className="star-input" role="radiogroup" aria-label={tr('Rating')}>
      {[1, 2, 3, 4, 5].map((n) => (
        <button
          key={n}
          type="button"
          role="radio"
          aria-checked={value === n}
          aria-label={tr(n === 1 ? '1 star' : '{count} stars', { count: n })}
          disabled={disabled}
          onClick={() => onChange(n)}
        >
          <Star size={22} fill={value >= n ? 'currentColor' : 'none'} />
        </button>
      ))}
    </div>
  );
}

export type ReviewTarget = { kind: 'user' | 'world' | 'avatar' | 'group'; id: string; name?: string };

/* Public ratings and comments for a VRChat user, world, avatar or group. Keyed so a new target starts fresh. */
export function Reviews(p: ReviewTarget) {
  useLocale();
  return <ReviewList key={p.kind + p.id} {...p} />;
}

export function ReviewList({ kind, id, name }: ReviewTarget) {
  useLocale();
  const [data, setData] = useState<any>(null),
    [error, setError] = useState(''),
    [offset, setOffset] = useState(0),
    [more, setMore] = useState<Review[]>([]);
  const [loadingMore, setLoadingMore] = useState(false);
  const [rating, setRating] = useState(0),
    [body, setBody] = useState(''),
    [editing, setEditing] = useState(false),
    [busy, setBusy] = useState(''),
    [formError, setFormError] = useState('');
  const base = '/cove/reviews?kind=' + kind + '&id=' + encodeURIComponent(id);
  const load = () =>
    api(base)
      .then((d) => {
        setError('');
        setData(d);
        setMore([]);
        setOffset(0);
        setRating(d.mine?.rating || 0);
        setBody(d.mine?.body || '');
      })
      .catch((e) => {
        setError(e.status === 503 ? 'disabled' : e.message);
      });
  // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once per target; ReviewList is keyed by it
  useEffect(() => {
    load();
  }, []);
  if (error === 'disabled') return null;
  async function save(e: React.FormEvent) {
    e.preventDefault();
    if (!rating) {
      setFormError(tr('Choose a rating.'));
      return;
    }
    setBusy('save');
    setFormError('');
    try {
      await api('/cove/review', { kind, id, rating, body, name: name || '' });
      setEditing(false);
      await load();
    } catch (e) {
      setFormError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  async function remove() {
    setBusy('delete');
    setFormError('');
    try {
      await api('/cove/review/delete', { kind, id });
      setEditing(false);
      await load();
    } catch (e) {
      setFormError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  async function loadMore() {
    if (loadingMore) return;
    setLoadingMore(true);
    const n = offset + data.pageSize;
    try {
      const d = await api(base + '&offset=' + n);
      setMore((m) => [...m, ...d.reviews]);
      setOffset(n);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoadingMore(false);
    }
  }
  const title = (
    <h3>
      <Star size={16} />
      {tr('Ratings & comments')}
      {data?.count > 0 && <small>{data.count}</small>}
    </h3>
  );
  if (!data)
    return (
      <section className="entity-section reviews">
        {title}
        {error ? (
          <p className="fineprint">{error}</p>
        ) : (
          <div className="loading-inline">
            <LoaderCircle size={14} className="spin" />
            {tr('Loading…')}
          </div>
        )}
      </section>
    );
  const reviews = [...data.reviews, ...more].filter((r) => !r.mine);
  const showForm = data.signedIn && data.linked && (!data.mine || editing);
  return (
    <section className="entity-section reviews">
      {title}
      {data.count > 0 && (
        <div className="review-summary">
          <div>
            <strong>{data.average.toFixed(1)}</strong>
            <Stars value={data.average} size={16} />
            <small>{tr(data.count === 1 ? '1 rating' : '{count} ratings', { count: data.count })}</small>
          </div>
          <ol>
            {[5, 4, 3, 2, 1].map((n) => (
              <li key={n}>
                <span>{n}</span>
                <i>
                  <b style={{ width: (data.distribution[n - 1] / data.count) * 100 + '%' }} />
                </i>
                <small>{data.distribution[n - 1]}</small>
              </li>
            ))}
          </ol>
        </div>
      )}
      {!data.signedIn && (
        <p className="fineprint">
          <a className="text-link" {...appLink('/account')}>
            {tr('Sign in with a VRCove account')}
          </a>{' '}
          {tr('to rate and comment. Your VRChat account is never shown.')}
        </p>
      )}
      {data.signedIn && !data.linked && (
        <p className="fineprint">
          <a className="text-link" {...appLink('/account')}>
            {tr('Link a VRChat account')}
          </a>{' '}
          {tr('to rate and comment.')}
        </p>
      )}
      {data.mine && !editing && (
        <article className="review mine">
          <header>
            <strong>{tr('Your review')}</strong>
            <Stars value={data.mine.rating} />
            <small>
              {day(data.mine.date)}
              {data.mine.edited ? ' · edited' : ''}
            </small>
          </header>
          {data.mine.body && <p>{data.mine.body}</p>}
          <div className="result-actions">
            <button className="standard-button" onClick={() => setEditing(true)}>
              {tr('Edit')}
            </button>
            <button className="standard-button danger-text" disabled={!!busy} onClick={remove}>
              {busy === 'delete' ? <LoaderCircle size={13} className="spin" /> : <Trash2 size={13} />}
              {tr('Delete')}
            </button>
          </div>
        </article>
      )}
      {showForm && (
        <form className="review-form" onSubmit={save}>
          <StarInput value={rating} onChange={setRating} disabled={!!busy} />
          <label className="notes-panel">
            <span className="sr-only">{tr('Comment')}</span>
            <textarea
              aria-label={tr('Comment')}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              maxLength={2000}
              rows={4}
              placeholder={tr('Add a comment (optional)')}
              disabled={!!busy}
            />
            <small>
              {body.length}
              {tr('/2000 · public, signed with your VRCove username')}
            </small>
          </label>
          {formError && (
            <div className="form-error" role="alert">
              {formError}
            </div>
          )}
          <div className="result-actions">
            <button className="primary" type="submit" disabled={!!busy}>
              {busy === 'save' ? <LoaderCircle size={14} className="spin" /> : <Check size={14} />}
              {data.mine ? tr('Save changes') : tr('Post review')}
            </button>
            {editing && (
              <button
                type="button"
                className="standard-button"
                onClick={() => {
                  setEditing(false);
                  setRating(data.mine.rating);
                  setBody(data.mine.body);
                  setFormError('');
                }}
              >
                {tr('Cancel')}
              </button>
            )}
          </div>
        </form>
      )}
      {reviews.length > 0 ? (
        <div className="review-list">
          {reviews.map((r) => (
            <article className="review" key={r.id}>
              <header>
                {r.profile ? (
                  <a className="review-author" {...appLink(covePath('cove', r.author))}>
                    {r.author}
                    <ArrowUpRight size={12} />
                  </a>
                ) : (
                  <strong>{r.author}</strong>
                )}
                <Stars value={r.rating} />
                <small>
                  {day(r.date)}
                  {r.edited ? ' · edited' : ''}
                </small>
                <ReportButton target={{ kind: 'review', id: r.id }} />
              </header>
              {r.body && <p>{r.body}</p>}
            </article>
          ))}
        </div>
      ) : (
        !data.mine && <p className="fineprint">{tr('No reviews yet.')}</p>
      )}
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {formError && !showForm && (
        <p className="form-error" role="alert">
          {formError}
        </p>
      )}
      {data.reviews.length + more.length < data.count && (
        <button className="standard-button" disabled={loadingMore} onClick={loadMore}>
          {loadingMore ? tr('Loading…') : tr('Load more')}
        </button>
      )}
    </section>
  );
}
