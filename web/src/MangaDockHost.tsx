import { Suspense, useEffect, useState } from 'react';
import { stored } from './ui';
import { lazyPage, PageBoundary, PageLoading } from './PageBoundary';
import { mangaOpenKey } from './mangaState';

const MangaDock = lazyPage(() => import('./MangaReader').then(m => ({ default: m.MangaDock })));

/** Mount the reader only while a book is open, including a persisted book. */
export default function MangaDockHost() {
  const [open, setOpen] = useState(() => !!stored(mangaOpenKey, null));
  useEffect(() => {
    const opened = () => setOpen(true), closed = () => setOpen(false);
    window.addEventListener('omnihub-manga-open', opened);
    window.addEventListener('omnihub-manga-closed', closed);
    return () => {
      window.removeEventListener('omnihub-manga-open', opened);
      window.removeEventListener('omnihub-manga-closed', closed);
    };
  }, []);
  return open ? <PageBoundary name="漫画阅读器"><Suspense fallback={<PageLoading />}><MangaDock /></Suspense></PageBoundary> : null;
}
