import { entityPath } from '../lib/routes';
import type { MouseEvent } from 'react';

export type Kind = 'world' | 'user' | 'group';

export type RawKind = Kind | 'avatar';

export type Target = { kind: Kind; id: string };

export const detailHref = (kind: Kind, id: string) => entityPath(kind, id);

/* Open a quick view through DetailHost, mounted in Atlas. Full pages use /world/<id>, /user/<id> and /group/<id>. */
export function openDetail(kind: Kind, id: string) {
  window.dispatchEvent(new CustomEvent<Target>('vrcove:detail', { detail: { kind, id } }));
}

/* Open the quick view on a plain click. Modified clicks and page-mode links open the full page. */
export function detailLink(kind: Kind, id: string, page = false) {
  return {
    href: detailHref(kind, id),
    onClick: (e: MouseEvent) => {
      if (page || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
      e.preventDefault();
      openDetail(kind, id);
    },
  };
}
