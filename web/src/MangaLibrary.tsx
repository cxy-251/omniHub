import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { api, post, request, type Status } from './api';
import VirtualGrid from './VirtualGrid';
import { chip, explain, notify, select, store, stored } from './ui';
import { MangaWindow } from './mangaWindow';

type Book = { metadata_error?: string; title: string; logical_path: string; library_name: string; read_only: boolean; total_pages: number };
type Page = { index: number; chapter_index: number; entry: string };
type PageList = { pages: Page[]; total: number; file_version: string };
type Position = { page_index: number; chapter_index: number; scroll_offset_ratio: number; position_mode?: string };
type Mode = 'strip' | 'single' | 'double';
type Recent = { title: string; logical_path: string; page_index: number };
type Boot = { total: number; version: string; userID: number };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

function BookReader({ book, active, onBack }: { book: Book; active: boolean; onBack(): void }) {
  const [boot, setBoot] = useState<Boot>();
  const [mode, setMode] = useState<Mode>('strip');
  const [focusRow, setFocusRow] = useState(0);
  const [revision, setRevision] = useState(0);
  const [position, setPosition] = useState<Position>();
  const [error, setError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [notice, setNotice] = useState('');
  const [coverPending, setCoverPending] = useState(false);
  const viewport = useRef<HTMLDivElement>(null);
  const pages = useRef(new Map<number, Page>());
  const restore = useRef<{ page: number; ratio: number } | undefined>(undefined);
  const adjustment = useRef<{ row: number; page: number; ratio: number; offset: number } | undefined>(undefined);
  const imageHeights = useRef(new Map<number, number>());
  const frame = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const alive = useRef(true);
  const queued = useRef<{ value: Position; boot: Boot } | undefined>(undefined);
  const sending = useRef(false);
  const lastSaved = useRef('');
  const latest = useRef<{ value: Position; boot: Boot } | undefined>(undefined);
  const selectedPage = useRef(0);
  const columns = mode === 'double' ? 2 : 1;
  const model = useMemo(() => boot ? new MangaWindow(Math.ceil(boot.total / columns)) : undefined, [boot, columns]);
  const current = useRef({ mode, model, focusRow });
  current.current = { mode, model, focusRow };
  const window = model ? mode === 'single' ? { start: focusRow, end: focusRow + 1 } : model.window(focusRow) : { start: 0, end: 0 };

  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);

  useEffect(() => {
    if (!active || boot) return;
    const controller = new AbortController();
    const path = encodeURIComponent(book.logical_path);
    Promise.all([
      api<PageList>(`/api/media/manga/page-list?path=${path}&offset=0&limit=1`, { signal: controller.signal }),
      api<Status>('/api/auth/status', { signal: controller.signal }),
      api<Position | null>(`/api/media/manga/progress?manga_id=${path}`, { signal: controller.signal })
        .catch((e: Error) => { if (!controller.signal.aborted) setSaveError(`断点恢复失败：${e.message}`); return null; }),
    ]).then(([list, status, saved]) => {
      if (controller.signal.aborted) return;
      if (!list.total) { setError('书包内没有正文图片'); return; }
      const initial = new MangaWindow(list.total);
      let target = { page: 0, ratio: 0 };
      if (saved?.position_mode === 'page') target = { page: Math.min(list.total - 1, Math.max(0, saved.page_index)), ratio: saved.scroll_offset_ratio };
      else if (saved) {
        const anchor = initial.locate(saved.scroll_offset_ratio * initial.total());
        target = { page: anchor.row, ratio: anchor.ratio };
        setNotice('历史整书断点已近似定位，后续按页内位置保存。');
      }
      for (const page of list.pages) pages.current.set(page.index, page);
      restore.current = target; selectedPage.current = target.page;
      setFocusRow(target.page); setBoot({ total: list.total, version: list.file_version, userID: status.user.id });
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active, book.logical_path, boot]);

  useEffect(() => {
    if (!active || !boot || !model) return;
    const first = window.start * columns, end = Math.min(boot.total, window.end * columns);
    const missing = Array.from({ length: end - first }, (_, index) => first + index).find(index => !pages.current.has(index));
    if (missing === undefined) return;
    const controller = new AbortController();
    api<PageList>(`/api/media/manga/page-list?path=${encodeURIComponent(book.logical_path)}&offset=${missing}&limit=32`, { signal: controller.signal })
      .then(list => {
        if (controller.signal.aborted) return;
        if (list.file_version !== boot.version || list.total !== boot.total) { setError('书包已改变，请重新打开。'); return; }
        for (const page of list.pages) pages.current.set(page.index, page);
        while (pages.current.size > 128) {
          const victim = [...pages.current.keys()].find(index => index < first || index >= end);
          if (victim === undefined) break;
          pages.current.delete(victim);
        }
        setRevision(value => value + 1);
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active, boot, model, book.logical_path, columns, window.start, window.end]);

  async function sendQueued() {
    if (sending.current) return;
    sending.current = true;
    try {
      while (queued.current) {
        const job = queued.current; queued.current = undefined;
        const body = JSON.stringify({ ...job.value, manga_id: book.logical_path, position_mode: 'page', file_version: job.boot.version });
        if (body === lastSaved.current) continue;
        try {
          await request('/api/media/manga/progress', { method: 'POST', keepalive: true, headers: { 'Content-Type': 'application/json', 'X-Omni-Account-ID': String(job.boot.userID) }, body });
          lastSaved.current = body;
          if (alive.current) setSaveError('');
        } catch (e) { if (alive.current) setSaveError(`断点保存失败：${(e as Error).message}`); }
      }
    } finally { sending.current = false; }
  }

  function flush() {
    if (latest.current?.boot.userID) { queued.current = latest.current; void sendQueued(); }
  }

  function capture() {
    const root = viewport.current, state = current.current;
    if (!root || !state.model || !boot || restore.current || !active) return;
    const anchor = state.mode === 'single' ? { row: state.focusRow, ratio: 0 } : state.model.locate(root.scrollTop);
    const rowColumns = state.mode === 'double' ? 2 : 1;
    const first = anchor.row * rowColumns;
    const index = selectedPage.current >= first && selectedPage.current < Math.min(boot.total, first + rowColumns) ? selectedPage.current : first;
    selectedPage.current = index;
    const image = root.querySelector<HTMLImageElement>(`img[data-page="${index}"]`);
    const page = pages.current.get(index);
    if (!page || !image?.naturalWidth || !image.clientHeight) return;
    const localOffset = root.scrollTop - (state.mode === 'single' ? 0 : state.model.prefix(anchor.row));
    const value = { page_index: index, chapter_index: page.chapter_index, scroll_offset_ratio: Math.max(0, Math.min(1, localOffset / image.clientHeight)), position_mode: 'page' };
    latest.current = { value, boot }; setPosition(value);
    if (state.mode !== 'single') setFocusRow(anchor.row);
    clearTimeout(timer.current); timer.current = setTimeout(flush, 500);
  }

  function scroll() {
    if (frame.current) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = 0;
      const root = viewport.current, state = current.current;
      if (!root || !state.model || !active) return;
      if (state.mode !== 'single') setFocusRow(state.model.locate(root.scrollTop).row);
      capture();
    });
  }

  function measure(row: number) {
    const root = viewport.current;
    if (!root || !model || !active || !root.clientWidth) return;
    const images = [...root.querySelectorAll<HTMLImageElement>(`[data-row="${row}"] img`)];
    const heights = images.filter(image => image.naturalWidth > 0).map(image => Math.min(65536, image.naturalHeight * image.clientWidth / image.naturalWidth));
    if (!heights.length) return;
    const anchorRow = mode === 'single' ? focusRow : model.locate(root.scrollTop).row;
    const first = anchorRow * columns;
    const page = selectedPage.current >= first && selectedPage.current < first + columns ? selectedPage.current : first;
    const offset = root.scrollTop - (mode === 'single' ? 0 : model.prefix(anchorRow));
    const previousHeight = imageHeights.current.get(page);
    const anchor = { row: anchorRow, page, ratio: previousHeight ? offset / previousHeight : 0, offset };
    for (const image of images) {
      if (!image.naturalWidth) continue;
      const index = Number(image.dataset.page);
      imageHeights.current.delete(index); imageHeights.current.set(index, Math.min(65536, image.naturalHeight * image.clientWidth / image.naturalWidth));
    }
    while (imageHeights.current.size > 128) {
      const victim = [...imageHeights.current.keys()].find(index => index < window.start * columns || index >= window.end * columns);
      if (victim === undefined) break;
      imageHeights.current.delete(victim);
    }
    const protectedRows = new Set(Array.from({ length: window.end - window.start }, (_, index) => window.start + index));
    if (model.measure(row, Math.max(...heights), protectedRows)) { adjustment.current = anchor; setRevision(value => value + 1); }
    else setRevision(value => value + 1);
  }

  useLayoutEffect(() => {
    const root = viewport.current;
    if (!active) {
      // Hidden elements report zero scroll geometry. Keep the last decoded
      // position so reopening can load its images before measuring again.
      if (latest.current) restore.current = { page: latest.current.value.page_index, ratio: latest.current.value.scroll_offset_ratio };
      return;
    }
    if (!root || !model) return;
    const target = restore.current;
    if (target) {
      const image = root.querySelector<HTMLImageElement>(`img[data-page="${target.page}"]`);
      if (image?.naturalWidth && image.clientHeight) {
        const row = Math.floor(target.page / columns);
        root.scrollTop = (mode === 'single' ? 0 : model.prefix(row)) + target.ratio * image.clientHeight;
        restore.current = undefined; adjustment.current = undefined;
        capture();
      }
    } else if (adjustment.current) {
      const anchor = adjustment.current; adjustment.current = undefined;
      const image = root.querySelector<HTMLImageElement>(`img[data-page="${anchor.page}"]`);
      const offset = anchor.ratio && image?.naturalWidth ? anchor.ratio * image.clientHeight : anchor.offset;
      root.scrollTop = (mode === 'single' ? 0 : model.prefix(anchor.row)) + offset;
      capture();
    }
  }, [active, model, revision, mode, columns, focusRow]);

  useEffect(() => {
    const root = viewport.current;
    if (!root || !active || !model) return;
    const observer = new ResizeObserver(() => {
      for (const row of root.querySelectorAll<HTMLElement>('[data-row]')) measure(Number(row.dataset.row));
    });
    observer.observe(root);
    return () => observer.disconnect();
  }, [active, model, mode, window.start, window.end]);

  useEffect(() => {
    if (!active) return;
    const hide = () => flush();
    globalThis.addEventListener('pagehide', hide);
    return () => { globalThis.removeEventListener('pagehide', hide); clearTimeout(timer.current); cancelAnimationFrame(frame.current); frame.current = 0; flush(); };
  }, [active, boot]);

  function goTo(page: number, nextMode = mode) {
    if (!boot) return;
    page = Math.max(0, Math.min(boot.total - 1, page));
    const ratio = page === latest.current?.value.page_index ? latest.current.value.scroll_offset_ratio : 0;
    restore.current = { page, ratio }; selectedPage.current = page; adjustment.current = undefined;
    setPosition(undefined); setError(''); setMode(nextMode); setFocusRow(Math.floor(page / (nextMode === 'double' ? 2 : 1))); setRevision(value => value + 1);
  }

  async function setCover(reset = false) {
    if (!boot?.userID) return;
    const page = pages.current.get(selectedPage.current);
    if (!reset && !page) return;
    setCoverPending(true);
    try {
      await request('/api/media/manga/custom-cover', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Omni-Account-ID': String(boot.userID) }, body: JSON.stringify({ path: book.logical_path, entry: reset ? '' : page!.entry }) });
      setNotice(reset ? '已恢复默认封面。' : '已将当前页设为封面。');
    } catch (e) { setError((e as Error).message); }
    finally { if (alive.current) setCoverPending(false); }
  }

  const pageIndex = position?.page_index ?? selectedPage.current;
  const currentPage = pages.current.get(pageIndex);
  const visibleEnd = model && viewport.current ? model.locate(viewport.current.scrollTop + viewport.current.clientHeight).row + 1 : focusRow + 2;
  return <section className="space-y-3">
    <div className="flex flex-wrap items-center gap-3"><button className={button} onClick={onBack}>返回书架</button><h3 className="break-all font-semibold">{book.title}</h3>
      <select disabled={!boot} className="rounded border border-line bg-surface p-2 text-sm" aria-label="漫画阅读模式" value={mode} onChange={event => goTo(pageIndex, event.target.value as Mode)}><option value="strip">条漫</option><option value="single">单页</option><option value="double">双页</option></select>
      {boot ? <><button className={button} disabled={Math.floor(pageIndex / columns) === 0} onClick={() => goTo(pageIndex - columns)}>上一页</button><button className={button} disabled={pageIndex + columns >= boot.total} onClick={() => goTo(pageIndex + columns)}>下一页</button>
        <label className="flex items-center gap-2 text-sm">跳至<input key={pageIndex} className="w-20 rounded border border-line bg-surface p-2" type="number" min={1} max={boot.total} defaultValue={pageIndex + 1} onBlur={event => { const page = Number(event.target.value); if (Number.isInteger(page) && page >= 1 && page <= boot.total) goTo(page - 1); }} />/ {boot.total} 页</label></> : null}
      {boot?.userID && !book.read_only ? <><button className={button} disabled={coverPending} onClick={() => void setCover()}>当前页设为封面</button><button className={button} disabled={coverPending} onClick={() => void setCover(true)}>默认封面</button></> : null}
      {boot && currentPage ? <a className={button} target="_blank" rel="noopener noreferrer" href={`/api/media/manga/page?${new URLSearchParams({ path: book.logical_path, entry: currentPage.entry, version: boot.version })}`}>打开原图</a> : null}
    </div>
    {notice ? <p role="status" className="text-sm text-muted">{notice}</p> : null}
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {saveError ? <p role="alert" className="whitespace-pre-wrap text-danger">{saveError}</p> : null}
    {!boot && !error ? <p role="status">正在打开书包…</p> : null}
    {boot && model ? <div ref={viewport} className="h-[calc(100vh-10rem)] min-h-80 overflow-auto overscroll-contain rounded-lg border border-line bg-black" onScroll={scroll} onWheel={() => { restore.current = undefined; }} onTouchStart={() => { restore.current = undefined; }}>
      {mode !== 'single' ? <div style={{ height: model.prefix(window.start) }} /> : null}
      {Array.from({ length: window.end - window.start }, (_, rowOffset) => {
        const row = window.start + rowOffset;
        return <div key={row} data-row={row} className="flex items-start gap-2" style={{ height: model.height(row) }}>
          {Array.from({ length: columns }, (_, column) => {
            const index = row * columns + column;
            if (index >= boot.total) return null;
            // A jump changes the window before scrollTop can move: load the
            // target first, then restore using its measured image height.
            const page = pages.current.get(index), visible = mode === 'single' || (restore.current ? Math.abs(row - focusRow) <= 1 : row >= focusRow - 1 && row <= visibleEnd);
            const src = active && visible && page ? `/api/media/manga/page?${new URLSearchParams({ path: book.logical_path, entry: page.entry, version: boot.version })}` : undefined;
            return <div key={index} className="min-w-0 flex-1"><img data-page={index} src={src} alt={`第 ${index + 1} 页`} className="w-full object-contain object-top" style={{ maxHeight: 65536 }} decoding="async" onLoad={() => measure(row)} onClick={() => { selectedPage.current = index; capture(); }} onError={event => { if (event.currentTarget.getAttribute('src')) setError('页面无法读取或解码；若书包已改变，请重新打开。'); }} /></div>;
          })}
        </div>;
      })}
      {mode !== 'single' ? <div style={{ height: model.total() - model.prefix(window.end) }} /> : null}
      <div style={{ height: viewport.current?.clientHeight ?? 600 }} />
    </div> : null}
    {position ? <p className="text-sm text-muted">第 {position.chapter_index + 1} 组 · 第 {position.page_index + 1} 页 · 页内 {(position.scroll_offset_ratio * 100).toFixed(1)}%</p> : null}
  </section>;
}

// MangaLibrary：书架一次取全（几百本，元数据很小，本页会话内缓存），在本地搜索与筛选：
// 书名/作者/标签关键字、标签栏（出现最多的标签）、♥ 我的点赞、连载未完；卡片上的标签点一下即按它筛选。
// 阅读器只挂载选中的那本，图片只加载可视区附近。
type ShelfBook = Book & { author?: string; tags?: string[] | null; is_complete?: boolean };
let shelfCache: ShelfBook[] | undefined;

async function loadShelf(signal: AbortSignal) {
  const all: ShelfBook[] = [];
  for (let offset = 0; ; offset += 100) {
    const response = await request(`/api/media/manga/list?offset=${offset}&limit=100`, { signal });
    const rows = await response.json() as ShelfBook[];
    all.push(...rows);
    if (response.headers.get('X-Has-More') !== 'true' || !rows.length) return all;
  }
}

export default function MangaLibrary({ active }: { active: boolean }) {
  const [books, setBooks] = useState<ShelfBook[] | undefined>(shelfCache);
  const [selected, setSelected] = useState<Book>();
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const [recent, setRecent] = useState<Recent[]>([]);
  const [likes, setLikes] = useState<Set<string>>(new Set());
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState(() => stored('omnihub.manga.filter', ''));   // '' 全部 | '__liked' | '__incomplete' | 标签
  useEffect(() => store('omnihub.manga.filter', filter), [filter]);
  useEffect(() => {
    if (!active || selected) return;
    const controller = new AbortController();
    api<Recent[]>('/api/media/manga/recent', { signal: controller.signal }).then(setRecent).catch(() => undefined);
    api<string[]>('/api/media/manga/likes', { signal: controller.signal }).then(list => setLikes(new Set(list))).catch(() => undefined);
    return () => controller.abort();
  }, [active, selected]);
  useEffect(() => {
    if (!active || (shelfCache && !revision)) return;
    const controller = new AbortController(); setError('');
    loadShelf(controller.signal).then(list => { shelfCache = list; setBooks(list); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active, revision]);
  const toggleLike = (book: ShelfBook) => {
    const next = !likes.has(book.logical_path);
    post('/api/media/manga/likes', { path: book.logical_path, liked: next })
      .then(() => setLikes(old => { const s = new Set(old); if (next) s.add(book.logical_path); else s.delete(book.logical_path); return s; }))
      .catch((e: Error) => notify(explain(e, '点赞')));
  };
  const all = books ?? [];
  const tagCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const b of all) for (const t of b.tags ?? []) counts.set(t, (counts.get(t) ?? 0) + 1);
    return [...counts].sort((a, b) => b[1] - a[1]).slice(0, 30);
  }, [all]);
  const needle = query.trim().toLowerCase();
  const shown = all.filter(b => (filter === '' ? true : filter === '__liked' ? likes.has(b.logical_path) : filter === '__incomplete' ? b.is_complete === false : (b.tags ?? []).includes(filter))
    && (!needle || b.title.toLowerCase().includes(needle) || (b.author ?? '').toLowerCase().includes(needle) || (b.tags ?? []).some(t => t.toLowerCase().includes(needle))));
  if (selected) return <BookReader key={selected.logical_path} book={selected} active={active} onBack={() => { setSelected(undefined); setRevision(value => value + 1); }} />;
  const incomplete = all.filter(b => b.is_complete === false).length;
  return <section className="space-y-2">
    <div className="flex flex-wrap items-center gap-1.5">
      <button className={chip(filter === '')} onClick={() => setFilter('')}>全部 {all.length}</button>
      <button className={chip(filter === '__liked')} onClick={() => setFilter('__liked')}>♥ 我的点赞 {likes.size}</button>
      {incomplete ? <button className={chip(filter === '__incomplete')} onClick={() => setFilter('__incomplete')}>连载/未完 {incomplete}</button> : null}
      <input className={`${select} ml-auto w-52`} placeholder="搜索书名、作者、标签" value={query} onChange={e => setQuery(e.target.value)} />
      <button className={button} onClick={() => setRevision(value => value + 1)}>刷新</button>
    </div>
    <div className="flex gap-1.5 overflow-x-auto pb-1">
      {tagCounts.map(([t, n]) => <button key={t} className={chip(filter === t)} onClick={() => setFilter(filter === t ? '' : t)}>🏷 {t} <span className="text-xs">{n}</span></button>)}
      {filter && !['__liked', '__incomplete'].includes(filter) && !tagCounts.some(([t]) => t === filter) ? <button className={chip(true)} onClick={() => setFilter('')}>🏷 {filter} ✕</button> : null}
    </div>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    {!books && !error ? <p role="status">正在读取书架…</p> : null}
    {recent.length && !filter && !needle ? <div className="space-y-1"><p className="text-sm text-muted">继续阅读</p>
      <div className="flex gap-2 overflow-x-auto pb-1">{recent.map(r => <button key={r.logical_path} className="w-24 shrink-0 overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary"
        onClick={() => setSelected({ title: r.title, logical_path: r.logical_path, library_name: '', read_only: true, total_pages: 0 })}>
        <img className="aspect-[3/4] w-full object-cover" loading="lazy" decoding="async" src={`/api/media/manga/cover?path=${encodeURIComponent(r.logical_path)}&revision=${revision}`} alt="" />
        <span className="block truncate px-1.5 pt-1 text-xs">{r.title}</span><span className="block px-1.5 pb-1 text-xs text-muted">第 {r.page_index + 1} 页</span>
      </button>)}</div></div> : null}
    {books && (filter || needle) ? <p className="text-xs text-muted">{shown.length} 本</p> : null}
    <VirtualGrid total={shown.length} minColumnWidth={150} aspect={3 / 4} extraHeight={64} render={i => {
      const book = shown[i];
      const liked = likes.has(book.logical_path);
      return <div className="relative flex h-full flex-col overflow-hidden rounded-lg border border-line bg-panel hover:border-primary">
        <button className="relative min-h-0 flex-1" onClick={() => setSelected(book)} title={book.title}>
          <img className="h-full w-full object-cover" loading="lazy" decoding="async" src={active ? `/api/media/manga/cover?path=${encodeURIComponent(book.logical_path)}&revision=${revision}` : undefined} alt="" />
          {book.is_complete === false ? <span className="absolute left-1 top-1 rounded bg-[#d29922] px-1 text-xs text-black">未完</span> : null}
        </button>
        <button className={`absolute right-1 top-1 rounded-full bg-black/60 px-1.5 text-sm ${liked ? 'text-[#f778ba]' : 'text-white/70'}`} onClick={() => toggleLike(book)} title={liked ? '取消点赞' : '点赞'}>{liked ? '♥' : '♡'}</button>
        <div className="h-16 px-1.5 py-1">
          <p className="truncate text-xs" title={book.title}>{book.title}</p>
          <p className="truncate text-xs text-muted">{book.metadata_error ? '页数未知' : `${book.total_pages} 页`}{book.author ? ` · ${book.author}` : ''}</p>
          <div className="flex gap-1 overflow-hidden">{(book.tags ?? []).slice(0, 3).map(t => <button key={t} className="shrink-0 rounded bg-background px-1 text-[11px] text-muted hover:text-primary" onClick={() => setFilter(t)}>{t}</button>)}</div>
        </div>
      </div>;
    }} />
    {books && !shown.length ? <p className="text-muted">{filter === '__liked' ? '还没有点赞的漫画。' : '没有匹配的漫画。'}</p> : null}
  </section>;
}
