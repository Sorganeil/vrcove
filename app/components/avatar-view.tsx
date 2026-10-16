import { useEffect } from 'react';
import {
  ArrowUpRight,
  Bookmark,
  ChevronLeft,
  Copy,
  ExternalLink,
  Heart,
  Maximize2,
  RefreshCw,
  ShieldCheck,
} from 'lucide-react';
import { formatDate, t as tr, useLocale } from '../lib/i18n';
import { entityPath, copyEntityLink } from '../lib/routes';
import { useApp } from '../lib/app-context';
import type { Avatar } from '../lib/avatar';
import { Picture, Platform } from './avatar-media';
import { useAvatarActions } from './avatar-actions';
import { RawJson, detailLink } from './vrchat-details';
import { Reviews } from '../pages/cove-account';
import { AddToCollection } from '../pages/cove-social';

/* One avatar, shown in the quick-view modal or as a full page (/avatar/<id>). */
export default function AvatarView({ avatar: a, page }: { avatar: Avatar; page: boolean }) {
  useLocale();
  const { user, notify, saved, bookmark, closeAvatar, browseAvatars } = useApp();
  const { busy, favoriteOf, loadFavorites, wear, favorite, fallback, unfavorite } = useAvatarActions();
  useEffect(() => {
    loadFavorites();
  }, [loadFavorites]);
  async function share() {
    try {
      await copyEntityLink('avatar', a.id);
      notify(tr('Link copied'));
    } catch {
      notify(tr('Clipboard access is unavailable.'));
    }
  }
  if (!page)
    return (
      <div className="detail detail-avatar">
        <div className="detail-image">
          <Picture src={a.image} fallbackSrc={a.thumbnail} alt={a.name} />
          <Platform a={a} />
        </div>
        <div className="detail-copy">
          <div className="detail-toolbar">
            <button className="text-link" onClick={share}>
              <Copy size={13} />
              {tr('Copy link')}
            </button>
            <a className="text-link" href={entityPath('avatar', a.id)} onClick={closeAvatar}>
              <Maximize2 size={13} />
              {tr('Open full page')}
            </a>
          </div>
          <div className="eyebrow">{tr('AVATAR / PUBLIC INDEX')}</div>
          <h2>{a.name}</h2>
          <p className="creator">
            {tr('by')}{' '}
            {a.authorId ? (
              <a {...detailLink('user', a.authorId)}>
                {a.author}
                <ArrowUpRight size={13} />
              </a>
            ) : (
              a.author
            )}
            {a.author && a.author !== 'Unknown creator' && (
              <button
                className="text-link more-by"
                onClick={() => browseAvatars({ author: { name: a.author, id: a.authorId } })}
              >
                {tr('More by {author}', { author: a.author })}
              </button>
            )}
          </p>
          <p className="description">{a.description || tr('The creator hasn’t added a description.')}</p>
          <div className="detail-meta">
            <span>
              {tr('PC performance')}
              <strong>{a.rating === 'VeryPoor' ? tr('Very poor') : a.rating}</strong>
            </span>
            <span>
              {tr('Last updated')}
              <strong>{formatDate(a.updated)}</strong>
            </span>
            <span>
              {tr('First created')}
              <strong>{formatDate(a.created)}</strong>
            </span>
          </div>
          {a.tags?.length > 0 && (
            <div className="tags">
              {a.tags.map((t, i) => {
                const label = t.replace(/^author_tag_/, '').replaceAll('_', ' ');
                return (
                  <button key={i} onClick={() => browseAvatars({ tag: label })}>
                    {label}
                  </button>
                );
              })}
            </div>
          )}
          <div className="detail-actions">
            <button className="primary" disabled={!!busy} onClick={() => wear(a)}>
              <RefreshCw size={16} />
              {tr('Wear this avatar')}
            </button>
            {favoriteOf(a) ? (
              <button disabled>
                <Heart size={16} fill="currentColor" />
                {tr('Favorited in VRChat')}
              </button>
            ) : (
              <button disabled={!!busy} onClick={() => favorite(a)}>
                <Heart size={16} />
                {tr('Favorite in VRChat')}
              </button>
            )}
            <button disabled={!!busy} onClick={() => fallback(a)}>
              <ShieldCheck size={16} />
              {tr('Set fallback avatar')}
            </button>
            <button onClick={() => bookmark(a)}>
              <Bookmark size={16} />
              {saved.some((s) => s.id === a.id) ? tr('Bookmarked') : tr('Bookmark on this device')}
            </button>
            <AddToCollection kind="avatar" target={a.id} name={a.name} image={a.thumbnail} className="" />
            {favoriteOf(a) && (
              <button disabled={!!busy} className="danger-text" onClick={() => unfavorite(a)}>
                {tr('Remove VRChat favorite')}
              </button>
            )}
          </div>
          <div className="avatar-id">
            <code>{a.id}</code>
            <button
              aria-label={tr('Copy avatar ID')}
              onClick={() =>
                navigator.clipboard
                  .writeText(a.id)
                  .then(() => notify(tr('Avatar ID copied')))
                  .catch(() => notify(tr('Clipboard access is unavailable.')))
              }
            >
              <Copy size={14} />
            </button>
          </div>
          <p className="fineprint">
            {tr(
              'This avatar comes from VRCove’s own database, so it may be out of date or no longer public.',
            )}
          </p>
          <RawJson
            kind="avatar"
            id={a.id}
            user={user}
            notify={notify}
            local={a}
            localLabel={tr('Our database')}
          />
          <Reviews kind="avatar" id={a.id} name={a.name} />
          <a
            className="text-link"
            href={'https://vrchat.com/home/avatar/' + a.id}
            target="_blank"
            rel="noreferrer"
          >
            {tr('View on VRChat')}
            <ExternalLink size={13} />
          </a>
        </div>
      </div>
    );
  return (
    <div className="entity-page">
      <div className="detail-toolbar">
        <button className="text-link" onClick={share}>
          <Copy size={13} />
          {tr('Copy link')}
        </button>
        <button className="text-link" onClick={() => window.history.back()}>
          <ChevronLeft size={14} />
          {tr('Back')}
        </button>
      </div>
      <div className="entity-hero avatar">
        <div className="entity-hero-image">
          <Picture src={a.image} fallbackSrc={a.thumbnail} alt={a.name} />
          <Platform a={a} />
        </div>
        <div className="entity-hero-copy">
          <div className="eyebrow">{tr('AVATAR / PUBLIC INDEX')}</div>
          <h1>{a.name}</h1>
          <p className="creator">
            {tr('by')}{' '}
            {a.authorId ? (
              <a {...detailLink('user', a.authorId)}>
                {a.author}
                <ArrowUpRight size={13} />
              </a>
            ) : (
              a.author
            )}
            {a.author && a.author !== 'Unknown creator' && (
              <button
                className="text-link more-by"
                onClick={() => browseAvatars({ author: { name: a.author, id: a.authorId } })}
              >
                {tr('More by {author}', { author: a.author })}
              </button>
            )}
          </p>
        </div>
      </div>
      <div className="entity-layout">
        <div className="entity-main">
          <p className="description">{a.description || tr('The creator hasn’t added a description.')}</p>
          {a.tags?.length > 0 && (
            <div className="tags">
              {a.tags.map((t, i) => {
                const label = t.replace(/^author_tag_/, '').replaceAll('_', ' ');
                return (
                  <button key={i} onClick={() => browseAvatars({ tag: label })}>
                    {label}
                  </button>
                );
              })}
            </div>
          )}
          <div className="detail-actions">
            <button className="primary" disabled={!!busy} onClick={() => wear(a)}>
              <RefreshCw size={16} />
              {tr('Wear this avatar')}
            </button>
            {favoriteOf(a) ? (
              <button disabled>
                <Heart size={16} fill="currentColor" />
                {tr('Favorited in VRChat')}
              </button>
            ) : (
              <button disabled={!!busy} onClick={() => favorite(a)}>
                <Heart size={16} />
                {tr('Favorite in VRChat')}
              </button>
            )}
            <button disabled={!!busy} onClick={() => fallback(a)}>
              <ShieldCheck size={16} />
              {tr('Set fallback avatar')}
            </button>
            <button onClick={() => bookmark(a)}>
              <Bookmark size={16} />
              {saved.some((s) => s.id === a.id) ? tr('Bookmarked') : tr('Bookmark on this device')}
            </button>
            <AddToCollection kind="avatar" target={a.id} name={a.name} image={a.thumbnail} className="" />
            {favoriteOf(a) && (
              <button disabled={!!busy} className="danger-text" onClick={() => unfavorite(a)}>
                {tr('Remove VRChat favorite')}
              </button>
            )}
          </div>
          <p className="fineprint">
            {tr(
              'This avatar comes from VRCove’s own database, so it may be out of date or no longer public.',
            )}
          </p>
          <RawJson
            kind="avatar"
            id={a.id}
            user={user}
            notify={notify}
            local={a}
            localLabel={tr('Our database')}
          />
          <Reviews kind="avatar" id={a.id} name={a.name} />
        </div>
        <aside className="entity-aside">
          <div className="detail-meta">
            <span>
              {tr('PC performance')}
              <strong>{a.rating === 'VeryPoor' ? tr('Very poor') : a.rating}</strong>
            </span>
            <span>
              {tr('Last updated')}
              <strong>{formatDate(a.updated)}</strong>
            </span>
            <span>
              {tr('First created')}
              <strong>{formatDate(a.created)}</strong>
            </span>
          </div>
          <div className="avatar-id">
            <code>{a.id}</code>
            <button
              aria-label={tr('Copy avatar ID')}
              onClick={() =>
                navigator.clipboard
                  .writeText(a.id)
                  .then(() => notify(tr('Avatar ID copied')))
                  .catch(() => notify(tr('Clipboard access is unavailable.')))
              }
            >
              <Copy size={14} />
            </button>
          </div>
          <a
            className="text-link"
            href={'https://vrchat.com/home/avatar/' + a.id}
            target="_blank"
            rel="noreferrer"
          >
            {tr('View on VRChat')}
            <ExternalLink size={13} />
          </a>
        </aside>
      </div>
    </div>
  );
}
