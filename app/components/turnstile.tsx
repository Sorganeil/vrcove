import { useEffect, useRef } from 'react';
import { t as tr, useLocale } from '../lib/i18n';

declare global {
  interface Window {
    turnstile?: {
      render: (el: HTMLElement, opts: Record<string, unknown>) => string;
      reset: (id: string) => void;
      remove: (id: string) => void;
    };
  }
}
let script: Promise<void> | null = null;
// Cloudflare's script loads only when the signup form is shown.
function loadScript() {
  script ??= new Promise<void>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    s.async = true;
    s.onload = () => resolve();
    s.onerror = () => {
      script = null;
      reject(new Error('Turnstile failed to load'));
    };
    document.head.appendChild(s);
  });
  return script;
}

/* Cloudflare Turnstile challenge for registration. `resetKey` changes after each attempt because tokens are single use. */
export default function Turnstile({
  siteKey,
  onToken,
  resetKey,
}: {
  siteKey: string;
  onToken: (token: string) => void;
  resetKey: number;
}) {
  const locale = useLocale();
  const box = useRef<HTMLDivElement>(null),
    widget = useRef(''),
    callback = useRef(onToken);
  useEffect(() => {
    callback.current = onToken;
  }, [onToken]);
  useEffect(() => {
    let alive = true;
    loadScript()
      .then(() => {
        if (!alive || !box.current || !window.turnstile) return;
        widget.current = window.turnstile.render(box.current, {
          sitekey: siteKey,
          action: 'signup',
          theme: 'dark',
          language: locale,
          callback: (t: string) => callback.current(t),
          'expired-callback': () => callback.current(''),
          'error-callback': () => callback.current(''),
        });
      })
      .catch(() => callback.current(''));
    return () => {
      alive = false;
      if (widget.current) window.turnstile?.remove(widget.current);
      widget.current = '';
    };
    // Re-rendering on a language change is not worth resetting a solved challenge.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [siteKey]);
  useEffect(() => {
    if (resetKey && widget.current) {
      callback.current('');
      window.turnstile?.reset(widget.current);
    }
  }, [resetKey]);
  return <div className="turnstile-box" ref={box} aria-label={tr('Verification check')} />;
}
