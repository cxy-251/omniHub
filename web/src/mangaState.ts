import { store } from './ui';

export type Book = { metadata_error?: string; title: string; logical_path: string; library_name: string; read_only: boolean; total_pages: number };
export const mangaOpenKey = 'omnihub.manga.open';

/** Global media shortcuts must yield to the reader's existing hover-based controls. */
export function mangaHandlesKeyboard() {
  const reader = document.querySelector<HTMLElement>('[data-manga-reader]');
  return !!reader && (reader.dataset.mangaReader === 'full' || reader.matches(':hover'));
}

/** The shelf can open a book without importing the reader implementation. */
export function openManga(book: Book) {
  store(mangaOpenKey, book);
  globalThis.dispatchEvent(new CustomEvent('omnihub-manga-open', { detail: book }));
}
