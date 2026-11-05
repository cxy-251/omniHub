import { useEffect, useRef } from 'react';

/** Poll serially while visible; abort on hide/unmount and refresh on return. */
export function useVisiblePolling(load: (signal: AbortSignal) => Promise<unknown>, interval: number | null) {
  const latest = useRef(load);
  latest.current = load;
  useEffect(() => {
    let timer = 0, stopped = false;
    let active: AbortController | undefined;
    const poll = async () => {
      if (stopped || active || document.hidden) return;
      const controller = new AbortController(); active = controller;
      try { await latest.current(controller.signal); }
      finally {
        if (active === controller) active = undefined;
        if (!stopped && !controller.signal.aborted && !document.hidden && interval !== null) timer = window.setTimeout(() => void poll(), interval);
      }
    };
    const visibility = () => {
      window.clearTimeout(timer);
      active?.abort(); active = undefined;
      if (!document.hidden) void poll();
    };
    void poll();
    document.addEventListener('visibilitychange', visibility);
    return () => { stopped = true; window.clearTimeout(timer); active?.abort(); document.removeEventListener('visibilitychange', visibility); };
  }, [interval]);
}
