import { LoaderCircle } from 'lucide-react';
import { useLocale } from '../lib/i18n';

export default function Loading({ text }: { text: string }) {
  useLocale();
  return (
    <div className="loading-state">
      <LoaderCircle className="spin" />
      {text}
    </div>
  );
}
