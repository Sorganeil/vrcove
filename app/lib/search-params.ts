/**
 * Changes some query parameters in the address bar without adding a history
 * entry, keeping the others, so a search can be shared or reloaded. null removes one.
 */
export function replaceSearchParams(patch: Record<string, string | null>) {
  const params = new URLSearchParams(window.location.search);
  for (const [key, value] of Object.entries(patch)) {
    if (value) params.set(key, value);
    else params.delete(key);
  }
  const next = window.location.pathname + (params.size ? '?' + params : '');
  if (window.location.pathname + window.location.search !== next) window.history.replaceState(null, '', next);
}
