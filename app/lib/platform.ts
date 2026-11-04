/**
 * True in the desktop app, which is built from this same frontend with
 * `vite build --mode desktop`. There VRChat requests go straight from the
 * device to VRChat, and a VRCove account is optional.
 */
export const desktop = import.meta.env.MODE === 'desktop';

/** Open an https link in the system browser because the desktop app has no tabs. */
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

const workerFlag =
  document.querySelector<HTMLMetaElement>('meta[name="vrcove-vrchat"]')?.content ||
  import.meta.env.VITE_VRCHAT_WORKER ||
  '';

/**
 * True when the website reaches VRChat through the VRChat worker
 * (lib/vrchat-worker.ts), with TLS that ends in this browser, instead of
 * through VRCove's server. The deployment turns it on with VRCHAT_WORKER.
 * chooseVRChatClient settles it before the app renders.
 */
export let vrchatInBrowser = false;

export async function chooseVRChatClient() {
  const supported =
    typeof WebAssembly === 'object' &&
    typeof Worker !== 'undefined' &&
    typeof indexedDB !== 'undefined' &&
    !!globalThis.crypto?.subtle;
  if (desktop || !workerFlag || !supported) return;
  // While both paths run, a browser already connected through the server
  // keeps that connection until it ends. New connections use the worker.
  if (workerFlag === 'on') {
    const current = await fetch('/api/account/me').catch(() => null);
    if (current?.ok) return;
  }
  vrchatInBrowser = true;
}

/** VRChat sessions live on this device: in the desktop app, or in the website's VRChat worker. */
export const deviceVRChat = () => desktop || vrchatInBrowser;
