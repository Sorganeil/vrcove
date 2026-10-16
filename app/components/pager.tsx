import { ChevronRight } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';

// Previous/next controls for VRChat lists, which page by 100.
export default function Pager({ offset, setOffset, count, busy }: any) {
  useLocale();
  return (
    <div className="pagination">
      <span>
        {tr('Up to 100 per page · starting at')} {offset + 1}
      </span>
      <div>
        <button disabled={!offset || busy} onClick={() => setOffset((p: number) => Math.max(0, p - 100))}>
          {tr('Previous')}
        </button>
        <button disabled={count < 100 || busy} onClick={() => setOffset((p: number) => p + 100)}>
          {tr('Next')}
          <ChevronRight size={15} />
        </button>
      </div>
    </div>
  );
}
