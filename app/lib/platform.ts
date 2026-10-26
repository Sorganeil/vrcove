/**
 * True in the desktop app, which is built from this same frontend with
 * `vite build --mode desktop`. There VRChat requests go straight from the
 * device to VRChat, and a VRCove account is optional.
 */
export const desktop = import.meta.env.MODE === 'desktop';

/** Opens an https link in the system browser; the desktop app has no tabs. */
export function openExternal(url: string) {
  void fetch('/api/desktop/open', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  });
}
