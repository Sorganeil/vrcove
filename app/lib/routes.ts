import { siteOrigin } from './platform';
export type EntityKind = 'avatar' | 'world' | 'user' | 'group';
export const entityPath = (kind: EntityKind, id: string) => '/' + kind + '/' + encodeURIComponent(id);
export const copyEntityLink = (kind: EntityKind, id: string) =>
  navigator.clipboard.writeText(siteOrigin + entityPath(kind, id));
export const covePath = (kind: 'cove' | 'collection', id: string) =>
  '/' + kind + '/' + encodeURIComponent(id);
export function goTo(path: string) {
  window.history.pushState(null, '', path);
  window.dispatchEvent(new PopStateEvent('popstate'));
}
// A real link to an in-app path: copying or middle-clicking gives the shareable URL, a plain click switches pages without reloading.
export function appLink(path: string, before?: () => void) {
  return {
    href: path,
    onClick: (e: {
      metaKey: boolean;
      ctrlKey: boolean;
      shiftKey: boolean;
      altKey: boolean;
      button: number;
      preventDefault: () => void;
    }) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button) return;
      e.preventDefault();
      before?.();
      goTo(path);
    },
  };
}

// Ask Atlas to look up the avatar and open its quick view.
export function openAvatar(id: string) {
  window.dispatchEvent(new CustomEvent<string>('vrcove:avatar', { detail: id }));
}
// Open an entity through the quick views in Atlas and DetailHost.
export function openQuickView(kind: EntityKind, id: string) {
  if (kind === 'avatar') openAvatar(id);
  else window.dispatchEvent(new CustomEvent('vrcove:detail', { detail: { kind, id } }));
}
