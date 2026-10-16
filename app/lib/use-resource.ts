import { useState, useEffect } from 'react';
import { api, ApiError } from './api';

export function useResource(path: string) {
  const [state, setState] = useState<{ path: string; data: any; error: string; status: number }>({
    path: '',
    data: null,
    error: '',
    status: 0,
  });
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let alive = true;
    api(path)
      .then((data) => {
        if (alive) setState({ path, data, error: '', status: 200 });
      })
      .catch((e: ApiError) => {
        if (alive) setState({ path, data: null, error: e.message, status: e.status });
      });
    return () => {
      alive = false;
    };
  }, [path, version]);
  const current = state.path === path ? state : { path, data: null, error: '', status: 0 };
  return { ...current, reload: () => setVersion((v) => v + 1) };
}
