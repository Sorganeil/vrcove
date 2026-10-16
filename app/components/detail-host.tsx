import { t as tr, useLocale } from '../lib/i18n';
import { useState, useEffect, useRef } from 'react';
import { Target } from './detail-links';
import { DetailView } from './detail-view';
import type { CoveAccount, VRChatUser } from '../lib/types';

export default function DetailHost({
  coveAccount,
  user,
  onLogin,
  notify,
}: {
  coveAccount: CoveAccount | null;
  user: VRChatUser | null;
  onLogin: () => void;
  notify: (message: string) => void;
}) {
  useLocale();
  const [stack, setStack] = useState<Target[]>([]);
  const ref = useRef<HTMLDivElement>(null);
  const target = stack.at(-1);
  useEffect(() => {
    const open = (e: Event) => {
      const t = (e as CustomEvent<Target>).detail;
      setStack((s) => (s.at(-1)?.id === t.id ? s : [...s, t].slice(-10)));
    };
    const close = () => setStack([]);
    window.addEventListener('vrcove:detail', open);
    window.addEventListener('hashchange', close);
    return () => {
      window.removeEventListener('vrcove:detail', open);
      window.removeEventListener('hashchange', close);
    };
  }, []);
  useEffect(() => {
    if (!target) return;
    ref.current?.focus();
    const old = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = old;
    };
  }, [target?.id]);
  if (!target) return null;
  const close = () => setStack([]);
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        className="modal wide"
        role="dialog"
        aria-modal="true"
        aria-label={target.kind + ' details'}
        tabIndex={-1}
        ref={ref}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.stopPropagation();
            close();
          }
        }}
      >
        <button className="modal-close" onClick={close} aria-label={tr('Close')}>
          ×
        </button>
        <DetailView
          key={target.kind + target.id}
          coveAccount={coveAccount}
          kind={target.kind}
          id={target.id}
          user={user}
          onLogin={() => {
            close();
            onLogin();
          }}
          notify={notify}
          mode="modal"
          onBack={stack.length > 1 ? () => setStack((s) => s.slice(0, -1)) : undefined}
          onClose={close}
        />
      </div>
    </div>
  );
}
