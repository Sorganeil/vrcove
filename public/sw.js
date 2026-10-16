// VRCove service worker. It caches only the app's own static files so the
// installed app opens quickly. API responses, images and account data are
// never cached; they always go to the network.
const CACHE = 'vrcove-shell-v1';
const SHELL = ['/', '/favicon.svg', '/vrcove-wordmark.svg', '/icon-192.png'];

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== CACHE).map(key => caches.delete(key)))).then(() => self.clients.claim()));
});

self.addEventListener('fetch', event => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/cdn-cgi/')) return;
  if (request.mode === 'navigate') {
    // Pages: always try the network first, fall back to the cached shell offline.
    event.respondWith(fetch(request).catch(() => caches.match('/')));
    return;
  }
  if (url.pathname.startsWith('/assets/')) {
    // Build files have content hashes in their names, so a cached copy never goes stale.
    event.respondWith(caches.match(request).then(hit => hit || fetch(request).then(response => {
      if (response.ok) { const copy = response.clone(); caches.open(CACHE).then(cache => cache.put(request, copy)); }
      return response;
    })));
  }
});
