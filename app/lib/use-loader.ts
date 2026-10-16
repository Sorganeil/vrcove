import { useEffect, useState } from 'react';

const sameDeps = (a: readonly unknown[], b: readonly unknown[]) =>
  a.length === b.length && a.every((v, i) => Object.is(v, b[i]));

/**
 * Loads data whenever `deps` change, ignoring responses that arrive after a newer request.
 * `load` receives `isAlive` so long multi-request loads can stop early.
 */
export function useLoader<T>(
  load: (isAlive: () => boolean) => Promise<T>,
  deps: readonly unknown[],
  initial: T,
) {
  const [data, setData] = useState<T>(initial);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [retries, setRetries] = useState(0);

  // A new request shows the loading state and clears the previous error.
  const [requested, setRequested] = useState({ deps, retries });
  if (!sameDeps(requested.deps, deps) || requested.retries !== retries) {
    setRequested({ deps, retries });
    setLoading(true);
    setError('');
  }

  useEffect(() => {
    let alive = true;
    load(() => alive)
      .then((result) => {
        if (alive) setData(result);
      })
      .catch((e) => {
        if (alive) setError((e as Error).message);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
    // `load` is recreated every render; `deps` lists what it actually reads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, retries]);

  return { data, setData, loading, error, retry: () => setRetries((r) => r + 1) };
}
