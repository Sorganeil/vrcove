import { api } from './api';
import { useState, useEffect } from 'react';

export function useLoad(path: string | null, user: any) {
  const [state, setState] = useState<{ path: string | null; data: any; error: string }>({
    path: null,
    data: null,
    error: '',
  });
  useEffect(() => {
    if (!path || !user) return;
    let alive = true;
    api(path)
      .then((data) => {
        if (alive) setState({ path, data, error: '' });
      })
      .catch((e) => {
        if (alive) setState({ path, data: null, error: e.message });
      });
    return () => {
      alive = false;
    };
  }, [path, user?.id]);
  return state.path === path ? state : { path, data: null, error: '' };
}
