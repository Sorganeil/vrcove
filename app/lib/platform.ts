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

/**
 * The release version from VERSION, such as v0.2.0. The website and the desktop
 * app are built from the same frontend, so they always show the same one.
 */
export const appVersion = (import.meta as { env?: { APP_VERSION?: string } }).env?.APP_VERSION || 'dev';

/**
 * The public website's origin for links people share or paste elsewhere. In
 * the desktop app the page's own origin is internal (wails://localhost).
 */
export const siteOrigin = desktop ? 'https://vrcove.st' : window.location.origin;
