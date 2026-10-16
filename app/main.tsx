import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import Atlas from './atlas';
import './globals.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Atlas />
  </StrictMode>,
);

// The service worker makes VRCove installable. Development skips it so
// cached files never hide local changes.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {});
  });
}
