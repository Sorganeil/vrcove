import { workerFetch } from './vrchat-client';
import { vrchatInBrowser } from './platform';

/**
 * Retries a VRChat image that failed through the site's image proxy. Images
 * that only a signed-in viewer may see then load through the VRChat worker,
 * which holds this browser's session, and show as a blob: URL. Returns false
 * when there is nothing to retry, so the caller shows its fallback.
 */
export function retryThroughWorker(img: HTMLImageElement, url: string): boolean {
  if (!vrchatInBrowser || img.dataset.worker) return false;
  let host = '';
  try {
    host = new URL(url).hostname;
  } catch {
    return false;
  }
  if (host !== 'api.vrchat.cloud') return false;
  img.dataset.worker = 'true';
  workerFetch('/api/image?url=' + encodeURIComponent(url))
    .then(async (response) => {
      if (!response.ok) throw new Error('image unavailable');
      const objectURL = URL.createObjectURL(await response.blob());
      img.addEventListener('load', () => URL.revokeObjectURL(objectURL), { once: true });
      img.src = objectURL;
    })
    // A second error reaches the caller's handler, which now shows its fallback.
    .catch(() => img.dispatchEvent(new Event('error')));
  return true;
}
