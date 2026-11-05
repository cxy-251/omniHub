import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react';
import { api, request, type Status } from './api';
import { chip, notify, store, stored } from './ui';
import { MangaWindow } from './mangaWindow';
import { mangaOpenKey as openKey, type Book } from './mangaState';

type Page = { index: number; chapter_index: number; entry: string };
type Chapter = { title: string; first_page: number; pages: number };
type PageList = { pages: Page[]; total: number; file_version: string; chapters?: Chapter[] };
type Position = { page_index: number; chapter_index: number; scroll_offset_ratio: number; position_mode?: string };
type Mode = 'strip' | 'single' | 'double';
type Boot = { total: number; version: string; userID: number; chapters: Chapter[] };
// 每本书各自记住的阅读方式与翻页方向（存在浏览器里）
type BookPrefs = { mode: Mode; dir: 'ltr' | 'rtl' };

// 卷/话的显示名：包里子文件夹的最后一段，常见写法换成「第 N 卷」「第 N 话」，其余原样。
function chapterName(c: Chapter, i: number) {
  const name = c.title.split('/').pop() ?? '';
  const vol = /^(?:vol(?:ume)?|v)[ ._-]*0*(\d+)$/i.exec(name) ?? /^第\s*0*(\d+)\s*卷$/.exec(name);
  if (vol) return `第 ${vol[1]} 卷`;
  const ch = /^(?:ch(?:apter)?|c|ep(?:isode)?)[ ._-]*0*(\d+)$/i.exec(name) ?? /^第\s*0*(\d+)\s*[话話回]$/.exec(name);
  if (ch) return `第 ${ch[1]} 话`;
  return name || `第 ${i + 1} 部分`;
}

// ---------- 打开与停靠 ----------
// 阅读器不属于某个标签页：书架里点开一本书后，它挂在页面最外层。
//   窗口放得下小窗（至少 320 像素）就能停靠在右侧，竖屏小窗、宽度可拖动，左边照常浏览 omniHub 的其他页面；也可以铺满整个页面。
//   窗口比 720 窄（手机竖屏）时默认全屏沉浸阅读，也能手动切成小窗。
// 当前打开的书存在浏览器里，刷新后还在。

// 小窗最窄 320 像素（拖动宽度的下限）。窗口能放下它就可以切成小窗，不看是什么设备；
// 窗口比 720 窄时默认全屏（手机竖屏），更宽默认小窗。手动切过的按窄窗、宽窗分别记住。
const minDock = 320;
function useWindowWidth() {
  const [width, setWidth] = useState(() => globalThis.innerWidth);
  useEffect(() => {
    const resize = () => setWidth(globalThis.innerWidth);
    globalThis.addEventListener('resize', resize);
    return () => globalThis.removeEventListener('resize', resize);
  }, []);
  return width;
}

export function MangaDock() {
  const [book, setBook] = useState<Book | undefined>(() => stored<Book | undefined>(openKey, undefined));
  const windowWidth = useWindowWidth();
  const canDock = windowWidth >= minDock;
  const fullKey = windowWidth < 720 ? 'omnihub.manga.full.narrow' : 'omnihub.manga.full.wide';
  const [fullPrefs, setFullPrefs] = useState<Record<string, boolean>>(() => ({}));
  const full = fullPrefs[fullKey] ?? stored(fullKey, windowWidth < 720);
  const toggleFull = () => { store(fullKey, !full); setFullPrefs(p => ({ ...p, [fullKey]: !full })); };
  const [width, setWidth] = useState(() => stored('omnihub.manga.dockWidth', 480));
  // 窗口变窄时小窗最多占 80%，左边至少留一些给其他页面
  const dockWidth = Math.round(Math.max(minDock, Math.min(width, windowWidth * 0.8)));
  useEffect(() => store('omnihub.manga.dockWidth', width), [width]);
  const docked = !!book && canDock && !full;
  useEffect(() => {
    const open = (event: Event) => setBook((event as CustomEvent<Book>).detail);
    globalThis.addEventListener('omnihub-manga-open', open);
    return () => globalThis.removeEventListener('omnihub-manga-open', open);
  }, []);
  // 停靠时把页面右边让出来（App 的最外层按 --manga-dock 留白）；铺满时锁住底下页面的滚动
  useEffect(() => {
    document.documentElement.style.setProperty('--manga-dock', docked ? `${dockWidth}px` : '0px');
    document.body.style.overflow = book && !docked ? 'hidden' : '';
    return () => { document.documentElement.style.setProperty('--manga-dock', '0px'); document.body.style.overflow = ''; };
  }, [book, docked, dockWidth]);
  if (!book) return null;
  const close = () => {
    store(openKey, undefined); setBook(undefined);
    if (document.fullscreenElement) void document.exitFullscreen();
    globalThis.dispatchEvent(new Event('omnihub-manga-closed'));
  };
  function resize(event: ReactMouseEvent<HTMLDivElement>) {
    event.preventDefault();
    const move = (e: MouseEvent) => setWidth(Math.round(Math.max(minDock, Math.min(globalThis.innerWidth * 0.8, globalThis.innerWidth - e.clientX))));
    const up = () => { globalThis.removeEventListener('mousemove', move); globalThis.removeEventListener('mouseup', up); document.body.style.userSelect = ''; };
    document.body.style.userSelect = 'none';
    globalThis.addEventListener('mousemove', move); globalThis.addEventListener('mouseup', up);
  }
  return <div data-manga-reader={docked ? 'docked' : 'full'} className={docked ? 'fixed bottom-0 right-0 top-0 z-40 border-l border-line' : 'fixed inset-0 z-50'} style={docked ? { width: dockWidth } : undefined}>
    {docked ? <div className="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize hover:bg-[#58a6ff]/40" title="拖动调整宽度" onMouseDown={resize} /> : null}
    <BookReader key={book.logical_path} book={book} docked={docked} canDock={canDock} onClose={close} onToggleDock={toggleFull} />
  </div>;
}

function BookReader({ book, docked, canDock, onClose, onToggleDock }: { book: Book; docked: boolean; canDock: boolean; onClose(): void; onToggleDock(): void }) {
  // 阅读器挂在全局的停靠层里（见 MangaDock），一直处于显示状态。
  const active = true;
  const prefsKey = `omnihub.manga.book.${book.logical_path}`;
  const [prefs, setPrefs] = useState<BookPrefs>(() => ({ mode: 'strip', dir: 'ltr', ...stored<Partial<BookPrefs>>(prefsKey, {}) }));
  useEffect(() => store(prefsKey, prefs), [prefsKey, prefs]);
  const [boot, setBoot] = useState<Boot>();
  const [mode, setModeState] = useState<Mode>(prefs.mode);
  const setMode = (m: Mode) => { setModeState(m); setPrefs(p => ({ ...p, mode: m })); };
  const [menu, setMenu] = useState(false);
  const [sheet, setSheet] = useState<'' | 'chapters' | 'mode' | 'more'>('');
  const [drag, setDrag] = useState<number>();
  const [background, setBackground] = useState(() => stored('omnihub.manga.background', '#000000'));
  useEffect(() => store('omnihub.manga.background', background), [background]);
  const shell = useRef<HTMLDivElement>(null);
  const hovered = useRef(false);
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
  const window = model ? mode === 'single' ? { start: focusRow, end: focusRow + 1 } : model.window(focusRow) : { start: 0, end: 0 };
  const layout = model && mode !== 'single' ? model.layout(window.start, window.end) : { top: 0, bottom: 0, origin: 0 };
  const previousOrigin = useRef(0);
  const current = useRef({ mode, model, focusRow, origin: layout.origin });
  current.current = { mode, model, focusRow, origin: layout.origin };

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
      setFocusRow(target.page); setBoot({ total: list.total, version: list.file_version, userID: status.user.id, chapters: list.chapters ?? [] });
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
    const anchor = state.mode === 'single' ? { row: state.focusRow, ratio: 0 } : state.model.locate(root.scrollTop + state.origin);
    const rowColumns = state.mode === 'double' ? 2 : 1;
    const first = anchor.row * rowColumns;
    const index = selectedPage.current >= first && selectedPage.current < Math.min(boot.total, first + rowColumns) ? selectedPage.current : first;
    selectedPage.current = index;
    const image = root.querySelector<HTMLImageElement>(`img[data-page="${index}"]`);
    const page = pages.current.get(index);
    if (!page || !image?.naturalWidth || !image.clientHeight) return;
    const localOffset = root.scrollTop + state.origin - (state.mode === 'single' ? 0 : state.model.prefix(anchor.row));
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
      if (!root || !state.model || !active || restore.current) return;
      if (state.mode !== 'single') setFocusRow(state.model.locate(root.scrollTop + state.origin).row);
      capture();
    });
  }

  function measure(row: number) {
    const root = viewport.current;
    if (!root || !model || !active || !root.clientWidth) return;
    const images = [...root.querySelectorAll<HTMLImageElement>(`[data-row="${row}"] img`)];
    const heights = images.filter(image => image.naturalWidth > 0).map(image => Math.min(65536, image.naturalHeight * image.clientWidth / image.naturalWidth));
    if (!heights.length) return;
    const anchorRow = mode === 'single' ? focusRow : model.locate(root.scrollTop + layout.origin).row;
    const first = anchorRow * columns;
    const page = selectedPage.current >= first && selectedPage.current < first + columns ? selectedPage.current : first;
    const offset = root.scrollTop + layout.origin - (mode === 'single' ? 0 : model.prefix(anchorRow));
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
    const oldOrigin = previousOrigin.current;
    previousOrigin.current = layout.origin;
    const target = restore.current;
    if (target) {
      const image = root.querySelector<HTMLImageElement>(`img[data-page="${target.page}"]`);
      if (image?.naturalWidth && image.clientHeight) {
        const row = Math.floor(target.page / columns);
        root.scrollTop = (mode === 'single' ? 0 : model.prefix(row) - layout.origin) + target.ratio * image.clientHeight;
        restore.current = undefined; adjustment.current = undefined;
        capture();
      }
    } else if (adjustment.current) {
      const anchor = adjustment.current; adjustment.current = undefined;
      const image = root.querySelector<HTMLImageElement>(`img[data-page="${anchor.page}"]`);
      const offset = anchor.ratio && image?.naturalWidth ? anchor.ratio * image.clientHeight : anchor.offset;
      root.scrollTop = (mode === 'single' ? 0 : model.prefix(anchor.row) - layout.origin) + offset;
      capture();
    } else if (oldOrigin !== layout.origin) {
      root.scrollTop += oldOrigin - layout.origin;
      capture();
    }
  }, [active, model, revision, mode, columns, focusRow, layout.origin]);

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
  // 当前所在的卷/话：按页码在各卷起始页里找（页信息可能还没取到）
  const chapters = boot?.chapters ?? [];
  const chapterIndex = chapters.reduce((found, c, i) => c.first_page <= pageIndex ? i : found, 0);
  const visibleEnd = model && viewport.current ? model.locate(viewport.current.scrollTop + layout.origin + viewport.current.clientHeight).row + 1 : focusRow + 2;
  const chapterLabel = chapters.length > 1 ? chapterName(chapters[chapterIndex], chapterIndex) : '';
  const unit = chapters.length > 1 && chapterName(chapters[0], 0).endsWith('卷') ? '卷' : '话';
  const prev = () => goTo(pageIndex - columns), next = () => goTo(pageIndex + columns);
  const hideMenu = () => { setMenu(false); setSheet(''); };
  // 点屏幕：中间三分之一呼出/收起菜单；单页、双页模式点左右两边翻页（按翻页方向）。
  function tap(event: ReactMouseEvent<HTMLDivElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    const x = (event.clientX - rect.left) / rect.width;
    if (mode === 'strip' || (x > 1 / 3 && x < 2 / 3)) { if (menu) hideMenu(); else setMenu(true); return; }
    hideMenu();
    if ((x <= 1 / 3) === (prefs.dir === 'ltr')) prev(); else next();
  }
  // 电脑上鼠标移到上下边缘时呼出菜单
  function hover(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.pointerType !== 'mouse' || menu) return;
    const rect = event.currentTarget.getBoundingClientRect();
    if (event.clientY - rect.top < 48 || rect.bottom - event.clientY < 64) setMenu(true);
  }
  // 键盘：鼠标在阅读器上（停靠小窗时）或全屏阅读时才响应，免得抢走旁边页面的按键
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!hovered.current && docked) return;
      const target = event.target as HTMLElement | null;
      if (target && (target.tagName === 'INPUT' || target.tagName === 'SELECT' || target.tagName === 'TEXTAREA')) return;
      const forward = prefs.dir === 'ltr' ? 'ArrowRight' : 'ArrowLeft', back = prefs.dir === 'ltr' ? 'ArrowLeft' : 'ArrowRight';
      if (event.key === 'Escape') { if (menu || sheet) hideMenu(); else if (document.fullscreenElement) void document.exitFullscreen(); else onClose(); }
      else if (mode !== 'strip' && (event.key === forward || event.key === ' ' || event.key === 'PageDown')) { event.preventDefault(); next(); }
      else if (mode !== 'strip' && (event.key === back || event.key === 'PageUp')) { event.preventDefault(); prev(); }
      else return;
    };
    globalThis.addEventListener('keydown', onKey);
    return () => globalThis.removeEventListener('keydown', onKey);
  });
  const fullscreen = () => { if (document.fullscreenElement) void document.exitFullscreen(); else void shell.current?.requestFullscreen?.().catch(() => notify('这个浏览器不支持网页全屏')); };
  // 「⋯」里的阅读工具。以后的工具（比如框选截取画面）加在这个列表里。
  const tools: { label: string; show: boolean; run(): void }[] = [
    { label: '当前页设为封面', show: !!boot?.userID && !book.read_only, run: () => void setCover() },
    { label: '恢复默认封面', show: !!boot?.userID && !book.read_only, run: () => void setCover(true) },
    { label: '打开原图', show: !!(boot && currentPage), run: () => { if (boot && currentPage) globalThis.open(`/api/media/manga/page?${new URLSearchParams({ path: book.logical_path, entry: currentPage.entry, version: boot.version })}`, '_blank', 'noopener'); } },
    { label: document.fullscreenElement ? '退出全屏' : '全屏', show: true, run: fullscreen },
  ];
  const sliderValue = drag ?? pageIndex;
  const dragChapter = chapters.reduce((found, c, i) => c.first_page <= sliderValue ? i : found, 0);
  const bar = 'pointer-events-auto bg-[#0d1117]/95 text-[#e6edf3] backdrop-blur';
  const iconButton = 'rounded-md px-3 py-2 text-sm hover:bg-white/10 disabled:opacity-40';
  return <div ref={shell} className="relative h-full w-full overflow-hidden" style={{ background }}
    onPointerMove={hover} onPointerEnter={() => { hovered.current = true; }} onPointerLeave={() => { hovered.current = false; }}>
    {boot && model ? <div ref={viewport} className="h-full overflow-auto overscroll-contain" style={{ overflowAnchor: 'none' }} onScroll={scroll} onClick={tap}
      onWheel={() => { restore.current = undefined; if (menu) hideMenu(); }} onTouchStart={() => { restore.current = undefined; }} onTouchMove={() => { if (menu) hideMenu(); }}>
      {mode !== 'single' ? <div style={{ height: layout.top }} /> : null}
      {Array.from({ length: window.end - window.start }, (_, rowOffset) => {
        const row = window.start + rowOffset;
        return <div key={row} data-row={row} className={`flex items-start ${prefs.dir === 'rtl' ? 'flex-row-reverse' : ''} ${mode === 'single' ? 'h-full items-center justify-center' : ''}`} style={mode === 'single' ? undefined : { height: model.height(row) }}>
          {Array.from({ length: columns }, (_, column) => {
            const index = row * columns + column;
            if (index >= boot.total) return null;
            // A jump changes the window before scrollTop can move: load the
            // target first, then restore using its measured image height.
            const page = pages.current.get(index), visible = mode === 'single' || (restore.current ? Math.abs(row - focusRow) <= 1 : row >= focusRow - 1 && row <= visibleEnd);
            const src = active && visible && page ? `/api/media/manga/page?${new URLSearchParams({ path: book.logical_path, entry: page.entry, version: boot.version })}` : undefined;
            return <div key={index} className={`min-w-0 flex-1 ${mode === 'single' ? 'flex h-full items-center justify-center' : ''}`}><img data-page={index} src={src} alt={`第 ${index + 1} 页`} className={mode === 'single' ? 'max-h-full max-w-full object-contain' : 'w-full object-contain object-top'} style={mode === 'single' ? undefined : { maxHeight: 65536 }} decoding="async" onLoad={() => measure(row)} onClick={() => { selectedPage.current = index; capture(); }} onError={event => { if (event.currentTarget.getAttribute('src')) setError('页面无法读取或解码；若书包已改变，请重新打开。'); }} /></div>;
          })}
        </div>;
      })}
      {mode !== 'single' ? <div style={{ height: layout.bottom }} /> : null}
      {mode !== 'single' ? <div style={{ height: viewport.current?.clientHeight ?? 600 }} /> : null}
    </div> : <div className="flex h-full items-center justify-center p-6 text-center text-sm text-[#8b949e]">{error || '正在打开书包…'}</div>}
    {/* 提示与错误浮在顶部，不占阅读区 */}
    {(notice || saveError || (error && boot)) ? <div className="pointer-events-none absolute inset-x-0 top-14 flex flex-col items-center gap-1 px-3">
      {[notice, saveError, boot ? error : ''].filter(Boolean).map(m => <p key={m} className="max-w-full rounded-md bg-black/75 px-3 py-1 text-xs text-white">{m}</p>)}
    </div> : null}
    {menu ? <div className="pointer-events-none absolute inset-0 flex flex-col justify-between">
      <div className={`${bar} flex items-center gap-1 border-b border-white/10 px-1 py-1`}>
        <button className={iconButton} onClick={onClose} title="返回书架（Esc）">←</button>
        <div className="min-w-0 flex-1"><p className="truncate text-sm font-semibold" title={book.title}>{book.title}</p>{chapterLabel ? <p className="truncate text-xs text-[#8b949e]">{chapterLabel}</p> : null}</div>
        {canDock ? <button className={iconButton} onClick={onToggleDock} title={docked ? '铺满整个页面阅读' : '缩成右侧小窗，左边照常浏览'}>{docked ? '⤢' : '⤡'}</button> : null}
        <button className={iconButton} onClick={() => setSheet(sheet === 'more' ? '' : 'more')} title="更多工具">⋯</button>
      </div>
      <div className={`${bar} space-y-1 border-t border-white/10 px-2 pb-2 pt-1`}>
        {boot ? <div className="flex items-center gap-2">
          {chapters.length > 1 ? <button className={iconButton} disabled={chapterIndex === 0} onClick={() => goTo(chapters[chapterIndex - 1].first_page)} title={`上一${unit}`}>⏮</button> : null}
          <input className="min-w-0 flex-1 accent-[#58a6ff]" type="range" min={0} max={boot.total - 1} value={sliderValue} aria-label="阅读进度"
            onChange={e => setDrag(Number(e.target.value))} onPointerUp={() => { if (drag !== undefined) { goTo(drag); setDrag(undefined); } }} onKeyUp={() => { if (drag !== undefined) { goTo(drag); setDrag(undefined); } }} />
          {chapters.length > 1 ? <button className={iconButton} disabled={chapterIndex >= chapters.length - 1} onClick={() => goTo(chapters[chapterIndex + 1].first_page)} title={`下一${unit}`}>⏭</button> : null}
        </div> : null}
        <div className="flex items-center gap-1 text-xs">
          <span className="min-w-0 flex-1 truncate text-[#8b949e]">{boot ? (chapters.length > 1 ? `${chapterName(chapters[dragChapter], dragChapter)} ${sliderValue - chapters[dragChapter].first_page + 1}/${chapters[dragChapter].pages} · ` : '') + `${sliderValue + 1}/${boot.total} 页` : ''}</span>
          {chapters.length > 1 ? <button className={iconButton} onClick={() => setSheet(sheet === 'chapters' ? '' : 'chapters')}>目录</button> : null}
          <button className={iconButton} onClick={() => setSheet(sheet === 'mode' ? '' : 'mode')}>{({ strip: '条漫', single: '单页', double: '双页' } as const)[mode]}</button>
        </div>
      </div>
    </div> : null}
    {menu && sheet ? <div className="absolute inset-x-0 bottom-24 mx-auto max-h-[60%] w-[min(26rem,calc(100%-1rem))] overflow-y-auto rounded-lg border border-white/10 bg-[#161b22]/95 p-2 text-sm text-[#e6edf3] shadow-xl backdrop-blur">
      {sheet === 'chapters' ? chapters.map((c, i) => <button key={i} className={`flex w-full justify-between rounded px-3 py-2 text-left hover:bg-white/10 ${i === chapterIndex ? 'text-[#58a6ff]' : ''}`} onClick={() => { goTo(c.first_page); hideMenu(); }}>
        <span>{chapterName(c, i)}</span><span className="text-xs text-[#8b949e]">{c.pages} 页</span></button>) : null}
      {sheet === 'mode' ? <div className="space-y-3 p-1">
        <div><p className="mb-1 text-xs text-[#8b949e]">阅读方式</p><div className="flex gap-1">{(['strip', 'single', 'double'] as const).map(m => <button key={m} className={chip(mode === m)} onClick={() => goTo(pageIndex, m)}>{({ strip: '条漫（上下滑）', single: '单页', double: '双页' })[m]}</button>)}</div></div>
        <div><p className="mb-1 text-xs text-[#8b949e]">翻页方向（单页、双页时点左右两边翻页）</p><div className="flex gap-1">
          <button className={chip(prefs.dir === 'ltr')} onClick={() => setPrefs(p => ({ ...p, dir: 'ltr' }))}>左开（点右边下一页）</button>
          <button className={chip(prefs.dir === 'rtl')} onClick={() => setPrefs(p => ({ ...p, dir: 'rtl' }))}>右开（日漫）</button></div></div>
        <div><p className="mb-1 text-xs text-[#8b949e]">背景</p><div className="flex gap-1">{[['#000000', '黑'], ['#30363d', '灰'], ['#f5f1e8', '米白']].map(([c, n]) => <button key={c} className={chip(background === c)} onClick={() => setBackground(c)}>{n}</button>)}</div></div>
      </div> : null}
      {sheet === 'more' ? <>{tools.filter(t => t.show).map(t => <button key={t.label} className="block w-full rounded px-3 py-2 text-left hover:bg-white/10" disabled={coverPending} onClick={() => { t.run(); setSheet(''); }}>{t.label}</button>)}</> : null}
    </div> : null}
  </div>;
}
