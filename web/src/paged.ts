import { useCallback, useEffect, useRef, useState } from 'react';
import { request } from './api';

// usePaged：按页懒加载的列表数据。item(i) 缺页时发请求；只保留最近访问的 maxPages 页，
// 远处的页丢弃；endpoint 变化或组件卸载时中止所有进行中的请求。
// 服务端以 X-Total-Count 返回总数，limit/offset 由这里追加。
export function usePaged<T>(endpoint: string, pageSize = 60, maxPages = 8) {
  const [total, setTotal] = useState(0);
  const [error, setError] = useState('');
  const [, setRevision] = useState(0);
  const pages = useRef(new Map<number, T[]>());
  const inflight = useRef(new Map<number, AbortController>());

  useEffect(() => {
    pages.current = new Map();
    setTotal(0); setError(''); setRevision(r => r + 1);
    const running = inflight.current;
    return () => { running.forEach(c => c.abort()); running.clear(); };
  }, [endpoint]);

  const fetchPage = useCallback((p: number) => {
    if (!endpoint || pages.current.has(p) || inflight.current.has(p)) return;
    const controller = new AbortController();
    inflight.current.set(p, controller);
    const sep = endpoint.includes('?') ? '&' : '?';
    request(`${endpoint}${sep}limit=${pageSize}&offset=${p * pageSize}`, { signal: controller.signal })
      .then(async response => {
        const rows = await response.json() as T[];
        if (controller.signal.aborted) return;
        const map = pages.current;
        map.delete(p); map.set(p, rows);              // Map 保持插入顺序，末尾最新
        while (map.size > maxPages) map.delete(map.keys().next().value!);
        setTotal(Number(response.headers.get('X-Total-Count') ?? rows.length));
        setError('');
        setRevision(r => r + 1);
      })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
      .finally(() => { if (inflight.current.get(p) === controller) inflight.current.delete(p); });
  }, [endpoint, pageSize, maxPages]);

  useEffect(() => { fetchPage(0); }, [fetchPage]);

  const item = useCallback((i: number): T | undefined => {
    const p = Math.floor(i / pageSize);
    const rows = pages.current.get(p);
    if (!rows) { fetchPage(p); return undefined; }
    pages.current.delete(p); pages.current.set(p, rows);  // 标记为最近访问
    return rows[i - p * pageSize];
  }, [fetchPage, pageSize]);

  return { total, error, item };
}
