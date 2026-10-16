import { useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';

// Accessible dialog: traps focus, closes on Escape or a backdrop click, and restores focus and scrolling.
export default function Modal({ children, close, title, wide = false }: any) {
  useLocale();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement,
      old = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    ref.current?.focus();
    const listener = (e: KeyboardEvent) => {
      if (ref.current !== Array.from(document.querySelectorAll('[role="dialog"]')).at(-1)) return;
      if (e.key === 'Escape') {
        e.stopImmediatePropagation();
        close();
      }
      if (e.key === 'Tab') {
        const nodes = Array.from(
            ref.current?.querySelectorAll<HTMLElement>(
              'button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled)',
            ) || [],
          ),
          first = nodes[0],
          last = nodes.at(-1);
        if (e.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) {
          e.preventDefault();
          last?.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener('keydown', listener);
    return () => {
      document.body.style.overflow = old;
      document.removeEventListener('keydown', listener);
      previous?.focus();
    };
  }, []);
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        ref={ref}
        tabIndex={-1}
        className={'modal ' + (wide ? 'wide' : '')}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <button className="modal-close" aria-label={tr('Close dialog')} onClick={close}>
          <X size={20} />
        </button>
        {children}
      </div>
    </div>
  );
}
