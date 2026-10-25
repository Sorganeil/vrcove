import type { ReactNode } from 'react';
import { Search, X } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';

/**
 * The search box every search page shares. Typing searches as you go; Enter
 * searches right away. `children` sit beside the box, such as a Filters button.
 */
export default function SearchBar({
  label,
  placeholder,
  value,
  onChange,
  onSubmit,
  children,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  onSubmit?: () => void;
  children?: ReactNode;
}) {
  useLocale();
  return (
    <form
      className="search-row"
      role="search"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit?.();
      }}
    >
      <div className="search-input">
        <Search size={19} />
        <input
          type="search"
          aria-label={tr(label)}
          placeholder={tr(placeholder)}
          value={value}
          maxLength={100}
          onChange={(e) => onChange(e.target.value)}
        />
        {value ? (
          <button type="button" aria-label={tr('Clear search')} onClick={() => onChange('')}>
            <X size={16} />
          </button>
        ) : (
          <kbd>/</kbd>
        )}
      </div>
      {children}
    </form>
  );
}

/** A sort menu: [value, label, disabled] options. */
export function SortMenu({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly (readonly [string, string, boolean?])[];
  onChange: (value: string) => void;
}) {
  useLocale();
  return (
    <label className="sort-label">
      {tr('Sort by')}
      <select aria-label={tr(label)} value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map(([option, text, disabled]) => (
          <option key={option} value={option} disabled={disabled}>
            {tr(text)}
          </option>
        ))}
      </select>
    </label>
  );
}

/** The input every search tab receives from the search page. */
export type SearchInput = {
  /** What is in the box right now. */
  input: string;
  setInput: (value: string) => void;
  /** The search that applies, a moment after typing stops. */
  query: string;
  /** Applies the box's text right away. */
  submit: () => void;
};
