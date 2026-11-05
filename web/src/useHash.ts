import { useSyncExternalStore } from 'react';

const subscribe = (notify: () => void) => {
  window.addEventListener('hashchange', notify);
  return () => window.removeEventListener('hashchange', notify);
};
const snapshot = () => window.location.hash;

/** Browser history stays the source of truth for independently addressable views. */
export function useHash() { return useSyncExternalStore(subscribe, snapshot); }
