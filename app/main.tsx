import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './app';
import { desktop } from './lib/platform';
import './globals.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

// The service worker makes VRCove installable. Development and the desktop
// app skip it so cached files never hide local changes.
if (import.meta.env.PROD && !desktop && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {});
  });
}
