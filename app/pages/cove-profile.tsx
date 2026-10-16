import { appLink } from '../lib/routes';
import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect } from 'react';
import { LoaderCircle, Library, Lock, Pencil, UserRound, Star } from 'lucide-react';
import { CovePicture } from '../components/cove-picture';
import { api } from '../lib/api';
import { day } from '../lib/dates';
import { Stars } from '../components/reviews';
import { Back, CollectionCard, Summary, kindLabel, targetLink } from '../components/cove-shared';
import { useResource } from '../lib/use-resource';
import type { Review } from '../lib/types';

/* /cove/<username>: a VRCove profile, visible to everyone when public. */
export function ProfilePage({ username }: { username: string }) {
  useLocale();
  const profile = useResource('/cove/profile?username=' + encodeURIComponent(username));
  const [more, setMore] = useState<Review[]>([]),
    [offset, setOffset] = useState(0),
    [loadingMore, setLoadingMore] = useState(false),
    [moreError, setMoreError] = useState('');
  useEffect(() => {
    setMore([]);
    setOffset(0);
  }, [username]);
  const p = profile.data;
  if (profile.error)
    return (
      <div className="entity-page">
        <Back />
        <div className="empty-state">
          <Lock size={30} />
          <h3>{profile.status === 404 ? tr('Profile unavailable') : tr('Couldn’t load this profile')}</h3>
          <p>{profile.error}</p>
        </div>
      </div>
    );
  if (!p)
    return (
      <div className="loading-state">
        <LoaderCircle className="spin" />
        {tr('Loading profile…')}
      </div>
    );
  async function loadMore() {
    if (loadingMore) return;
    setLoadingMore(true);
    setMoreError('');
    try {
      const n = offset + p.pageSize;
      const d = await api('/cove/profile?username=' + encodeURIComponent(username) + '&offset=' + n);
      setMore((m) => [...m, ...d.reviews]);
      setOffset(n);
    } catch (e) {
      setMoreError((e as Error).message);
    } finally {
      setLoadingMore(false);
    }
  }
  const reviews = [...p.reviews, ...more];
  return (
    <div className="entity-page cove-profile">
      <Back />
      <div className="cove-profile-hero">
        <CovePicture picture={p.picture} username={p.username} size={72} />
        <div>
          <div className="eyebrow">
            <UserRound size={13} />
            {tr('VRCOVE PROFILE')}
            {p.public ? '' : ' / PRIVATE'}
          </div>
          <h1>{p.username}</h1>
          {p.status && <p className="cove-profile-status">{p.status}</p>}
          <p className="creator">
            {tr('Member since')} {day(p.created)} ·{' '}
            {tr(p.reviewCount === 1 ? '1 review' : '{count} reviews', { count: p.reviewCount })}
            {p.reviewCount > 0 && (
              <>
                {' '}
                {tr('· average given')} {p.averageGiven.toFixed(1)}
                <Star size={12} />
              </>
            )}
          </p>
        </div>
      </div>
      {p.mine && (
        <a className="standard-button cove-profile-edit" {...appLink('/account')}>
          <Pencil size={14} />
          {tr('Edit VRCove profile')}
        </a>
      )}
      {p.mine && !p.public && (
        <div className="privacy-note">
          <Lock size={18} />
          <p>
            {tr('Only you can see this page. Make your profile public in')}{' '}
            <a className="text-link" {...appLink('/account')}>
              {tr('your VRCove account')}
            </a>{' '}
            {tr('to share it.')}
          </p>
        </div>
      )}
      {p.bio && (
        <section className="entity-section cove-about">
          <h3>{tr('About')}</h3>
          <p className="description cove-bio">{p.bio}</p>
        </section>
      )}
      <section className="entity-section">
        <h3>
          <Library size={16} />
          {tr('Collections')}
          {p.collections.length > 0 && <small>{p.collections.length}</small>}
        </h3>
        {p.collections.length ? (
          <div className="entity-grid">
            {p.collections.map((c: Summary) => (
              <CollectionCard key={c.id} c={c} />
            ))}
          </div>
        ) : (
          <p className="fineprint">
            {p.mine ? (
              <>
                {tr('No collections yet.')}{' '}
                <a className="text-link" {...appLink('/collections')}>
                  {tr('Make one')}
                </a>
                .
              </>
            ) : (
              tr('No public collections.')
            )}
          </p>
        )}
      </section>
      <section className="entity-section reviews">
        <h3>
          <Star size={16} />
          {tr('Reviews')}
          {p.reviewCount > 0 && <small>{p.reviewCount}</small>}
        </h3>
        {reviews.length ? (
          <div className="review-list">
            {reviews.map((r: Review & { target: string }) => (
              <article className="review" key={r.kind + r.target}>
                <header>
                  <span className="review-kind">{tr(kindLabel[r.kind])}</span>
                  <a className="review-author" {...targetLink(r.kind, r.target)}>
                    {r.name || r.target}
                  </a>
                  <Stars value={r.rating} />
                  <small>
                    {day(r.date)}
                    {r.edited ? ' · edited' : ''}
                  </small>
                </header>
                {r.body && <p>{r.body}</p>}
              </article>
            ))}
          </div>
        ) : (
          <p className="fineprint">{tr('No reviews yet.')}</p>
        )}
        {moreError && (
          <p className="form-error" role="alert">
            {moreError}
          </p>
        )}
        {reviews.length < p.reviewCount && (
          <button className="standard-button" disabled={loadingMore} onClick={loadMore}>
            {loadingMore ? tr('Loading…') : tr('Load more')}
          </button>
        )}
      </section>
    </div>
  );
}
