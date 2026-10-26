import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './app';
import { desktop, openExternal } from './lib/platform';
import './globals.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

// The service worker makes VRCove installable. Development and the desktop
// app skip it so cached files never hide local changes.
// The desktop app opens links to other sites in the system browser.
if (desktop) {
  document.addEventListener(
    'click',
    (e) => {
      const link = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null;
      if (
        !link ||
        e.defaultPrevented ||
        !/^https:\/\//.test(link.href) ||
        link.origin === window.location.origin
      )
        return;
      e.preventDefault();
      openExternal(link.href);
    },
    true,
  );
}

if (import.meta.env.PROD && !desktop && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {});
  });
}
