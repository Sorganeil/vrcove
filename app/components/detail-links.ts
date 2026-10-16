import { entityPath } from '../lib/routes';

export type Kind = 'world' | 'user' | 'group';

export type RawKind = Kind | 'avatar';

export type Target = { kind: Kind; id: string };

export const detailHref = (kind: Kind, id: string) => entityPath(kind, id);

/* Quick-view modal from anywhere; DetailHost (mounted once in Atlas) renders it. Full pages live at /world/<id>, /user/<id>, /group/<id>. */
export function openDetail(kind: Kind, id: string) {
  window.dispatchEvent(new CustomEvent<Target>('vrcove:detail', { detail: { kind, id } }));
}

/* A plain click opens the quick view; modified clicks (new tab, etc.) and page-mode links go to the full page. */
export function detailLink(kind: Kind, id: string, page = false) {
  return {
    href: detailHref(kind, id),
    onClick: (e: any) => {
      if (page || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
      e.preventDefault();
      openDetail(kind, id);
    },
  };
}
