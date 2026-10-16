import { useEffect, useRef, useState } from 'react';
import { api, request } from './api';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';
import { button, chip, select, store, stored } from './ui';

// 文本页：小说（书架 + EPUB/TXT 阅读器，进度与桌面端共用）与文档（media_library/docs 目录浏览，md/rst 整篇渲染）。
type Novel = { id: string; title: string; category: string; format: string; library_id: string; chapter_index?: number };
type Category = { category: string; count: number };

export default function TextPage({ active }: { active: boolean }) {
  const [part, setPart] = useState<'novels' | 'docs'>(() => linkedDoc() ? 'docs' : linkedBook() ? 'novels' : stored('omnihub.text.part', 'novels'));
  useEffect(() => store('omnihub.text.part', part), [part]);
  if (!active) return null;
  return <section className="space-y-2">
    <div className="flex gap-1.5">
      <button className={chip(part === 'novels')} onClick={() => { setPart('novels'); window.history.replaceState(null, '', '#text'); }}>小说</button>
      <button className={chip(part === 'docs')} onClick={() => setPart('docs')}>文档</button>
    </div>
    {part === 'novels' ? <Novels /> : <Docs />}
  </section>;
}

// ---------- 小说 ----------
// 每本书有可分享的地址 #text/book/<书 ID>。
const bookPrefix = '#text/book/';
function linkedBook(): Novel | undefined {
  if (!window.location.hash.startsWith(bookPrefix)) return undefined;
  const id = decodeURIComponent(window.location.hash.slice(bookPrefix.length));
  const name = id.slice(id.lastIndexOf('/') + 1), dot = name.lastIndexOf('.');
  return { id, title: dot > 0 ? name.slice(0, dot) : name, category: '', format: dot > 0 ? name.slice(dot + 1).toLowerCase() : '', library_id: id.slice(0, id.indexOf(':')) };
}
function Novels() {
  const [categories, setCategories] = useState<Category[]>([]);
  const [recent, setRecent] = useState<Novel[]>([]);
  const [category, setCategory] = useState(() => stored('omnihub.text.category', '__recent'));
  const [book, setBook] = useState<Novel | undefined>(linkedBook);
  const [error, setError] = useState('');
  useEffect(() => store('omnihub.text.category', category), [category]);
  useEffect(() => {
    const hash = book ? bookPrefix + encodeURIComponent(book.id) : '#text';
    if (window.location.hash !== hash) window.history.replaceState(null, '', hash);
  }, [book]);
  useEffect(() => {
    if (book) return;
    const controller = new AbortController();
    api<Category[] | null>('/api/media/text/novel-categories', { signal: controller.signal }).then(list => setCategories(list ?? [])).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    api<Novel[]>('/api/media/text/recent', { signal: controller.signal }).then(setRecent).catch(() => undefined);
    return () => controller.abort();
  }, [book]);
  const showRecent = category === '__recent';
  const paged = usePaged<Novel>(!book && !showRecent ? `/api/media/text/novels?category=${encodeURIComponent(category)}` : '');
  if (book) return <BookReader book={book} onClose={() => setBook(undefined)} />;
  const total = showRecent ? recent.length : paged.total;
  const item = (i: number) => showRecent ? recent[i] : paged.item(i);
  return <div className="space-y-2">
    <div className="flex gap-1.5 overflow-x-auto pb-1">
      <button className={chip(showRecent)} onClick={() => setCategory('__recent')}>最近阅读</button>
      {categories.map(c => <button key={c.category} className={chip(c.category === category)} onClick={() => setCategory(c.category)}>{c.category.split('/').pop() || '未分类'} <span className="text-xs">{c.count}</span></button>)}
    </div>
    {error || paged.error ? <p className="text-sm text-danger">{error || paged.error}</p> : null}
    {showRecent && !recent.length ? <p className="text-sm text-muted">{categories.length ? '还没有阅读记录（登录后才会记录进度）。从上面选一个分类。' : '没有小说。'}</p> : null}
    <VirtualGrid total={total} minColumnWidth={130} aspect={3 / 4} extraHeight={40} render={i => {
      const b = item(i);
      if (!b) return <div className="h-full rounded-lg border border-line bg-panel" />;
      return <button className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary" onClick={() => setBook(b)}>
        <div className="relative min-h-0 flex-1">
          <BookCover title={b.title} />
          {b.format === 'epub' ? <img className="absolute inset-0 h-full w-full object-cover" src={`/api/media/text/epub/cover?path=${encodeURIComponent(b.id)}`} alt="" loading="lazy" decoding="async" /> : null}
        </div>
        <div className="h-10 px-1.5 py-0.5"><p className="truncate text-xs">{b.title}</p>
          <p className="truncate text-xs text-muted">{showRecent ? `读到第 ${(b.chapter_index ?? 0) + 1} 章` : b.format.toUpperCase()}</p></div>
      </button>;
    }} />
  </div>;
}

// 没有封面时按书名生成色块封面（EPUB 无封面时接口返回透明图，盖在上面不影响）。
function BookCover({ title }: { title: string }) {
  let h = 0;
  for (const c of title) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return <div className="flex h-full items-center justify-center p-2 text-center text-sm font-semibold text-white/90" style={{ background: `hsl(${h % 360} 35% 28%)` }}>{title}</div>;
}

type Epub = { title: string; author: string; asset_base: string; chapters: { title: string; entry: string }[] };
type Saved = { position: { start_line: number; byte_offset: number; chapter_index: number; scroll_ratio: number } | null; can_save: boolean; file_version?: string };

function BookReader({ book, onClose }: { book: Novel; onClose(): void }) {
  const [saved, setSaved] = useState<Saved>();
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    api<Saved>(`/api/media/text/progress?document_id=${encodeURIComponent(book.id)}`, { signal: controller.signal })
      .then(setSaved).catch((e: Error) => { if (!controller.signal.aborted) { setError(`读取进度失败：${e.message}`); setSaved({ position: null, can_save: false }); } });
    return () => controller.abort();
  }, [book.id]);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);
  const save = (body: Record<string, number>) => {
    if (!saved?.can_save) return;
    request('/api/media/text/progress', { method: 'POST', headers: { 'Content-Type': 'application/json' }, keepalive: true,
      body: JSON.stringify({ document_id: book.id, start_line: 1, byte_offset: 0, chapter_index: 0, scroll_ratio: 0, ...body }) })
      .then(() => setError('')).catch((e: Error) => setError(`进度保存失败：${e.message}`));
  };
  return <div className="space-y-2">
    <div className="flex flex-wrap items-center gap-2">
      <button className={button} onClick={onClose}>← 书架</button>
      <h2 className="min-w-0 flex-1 truncate font-semibold">{book.title}</h2>
      {saved && !saved.can_save ? <span className="text-xs text-muted">未登录，不记录进度</span> : null}
      {error ? <span className="text-xs text-danger">{error}</span> : null}
    </div>
    {!saved ? <p role="status">正在读取…</p> : book.format === 'epub' ? <EpubReader book={book} saved={saved} save={save} /> : <TxtReader book={book} saved={saved} save={save} />}
  </div>;
}

// 阅读器外观（字号、夜间）只存在本浏览器。
function useLook() {
  const [size, setSize] = useState(() => stored('omnihub.text.size', 18));
  const [night, setNight] = useState(() => stored('omnihub.text.night', true));
  useEffect(() => store('omnihub.text.size', size), [size]);
  useEffect(() => store('omnihub.text.night', night), [night]);
  const controls = <>
    <button className={button} onClick={() => setSize(s => Math.max(12, s - 2))} title="字号减小">A−</button>
    <span className="text-xs text-muted">{size}</span>
    <button className={button} onClick={() => setSize(s => Math.min(32, s + 2))} title="字号增大">A+</button>
    <button className={button} onClick={() => setNight(n => !n)}>{night ? '☾ 夜间' : '☀ 日间'}</button>
  </>;
  return { size, night, controls };
}

function EpubReader({ book, saved, save }: { book: Novel; saved: Saved; save(body: Record<string, number>): void }) {
  const [info, setInfo] = useState<Epub>();
  const [chapter, setChapter] = useState(saved.position?.chapter_index ?? 0);
  const [error, setError] = useState('');
  const frame = useRef<HTMLIFrameElement>(null);
  const restore = useRef(saved.position?.scroll_ratio ?? 0);
  const ratio = useRef(0);
  const look = useLook();
  useEffect(() => {
    const controller = new AbortController();
    api<Epub>(`/api/media/text/epub/book?path=${encodeURIComponent(book.id)}`, { signal: controller.signal })
      .then(next => { setInfo(next); setChapter(c => Math.min(c, Math.max(0, next.chapters.length - 1))); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [book.id]);
  // 翻章立即存一次；章内滚动停下 1 秒后存；离开时再存。
  const latest = useRef({ chapter, save });
  latest.current = { chapter, save };
  useEffect(() => () => latest.current.save({ chapter_index: latest.current.chapter, scroll_ratio: ratio.current }), []);
  const go = (index: number) => { if (!info || index < 0 || index >= info.chapters.length) return; restore.current = 0; ratio.current = 0; setChapter(index); save({ chapter_index: index, scroll_ratio: 0 }); };
  const keys = useRef(go);
  keys.current = go;
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement).tagName === 'SELECT') return;
      if (e.key === 'ArrowLeft') keys.current(latest.current.chapter - 1);
      else if (e.key === 'ArrowRight') keys.current(latest.current.chapter + 1);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  // 章节页面同源（sandbox 允许同源、禁止脚本），加载后注入字号与配色，并跟踪滚动位置。
  const styleFrame = () => {
    const doc = frame.current?.contentDocument;
    if (!doc?.documentElement) return;
    let style = doc.getElementById('omnihub-look') as HTMLStyleElement | null;
    if (!style) { style = doc.createElement('style'); style.id = 'omnihub-look'; doc.head?.appendChild(style); }
    style.textContent = `html{font-size:${look.size}px!important}body{max-width:46em;margin:0 auto!important;padding:1em 1.2em 4em!important;line-height:1.8!important;font-size:1rem!important;
      ${look.night ? 'background:#0d1117!important;color:#c9d1d9!important;color-scheme:dark}body *{color:inherit!important;background-color:transparent!important;border-color:#30363d!important}a{color:#58a6ff!important}' : 'background:#fbf8f1!important;color:#222!important}'}img{max-width:100%;height:auto}`;
  };
  useEffect(styleFrame, [look.size, look.night]); // eslint-disable-line react-hooks/exhaustive-deps
  const onLoad = () => {
    styleFrame();
    const win = frame.current?.contentWindow, doc = frame.current?.contentDocument;
    if (!win || !doc) return;
    const max = () => Math.max(1, doc.documentElement.scrollHeight - win.innerHeight);
    if (restore.current > 0) win.scrollTo(0, restore.current * max());
    let timer = 0;
    win.addEventListener('scroll', () => {
      ratio.current = Math.min(1, win.scrollY / max());
      window.clearTimeout(timer);
      timer = window.setTimeout(() => latest.current.save({ chapter_index: latest.current.chapter, scroll_ratio: ratio.current }), 1000);
    });
    win.addEventListener('keydown', e => { if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') window.dispatchEvent(new KeyboardEvent('keydown', { key: e.key })); });
  };
  if (error) return <p className="text-danger">{error}</p>;
  if (!info) return <p role="status">正在打开…</p>;
  if (!info.chapters.length) return <p className="text-muted">这本书没有可显示的章节。</p>;
  return <div className="space-y-2">
    <div className="flex flex-wrap items-center gap-2">
      <button className={button} disabled={chapter === 0} onClick={() => go(chapter - 1)} title="上一章（←）">上一章</button>
      <select aria-label="目录" className={`${select} min-w-0 max-w-[60vw]`} value={chapter} onChange={e => go(Number(e.target.value))}>
        {info.chapters.map((c, i) => <option key={i} value={i}>{i + 1}. {c.title}</option>)}
      </select>
      <button className={button} disabled={chapter >= info.chapters.length - 1} onClick={() => go(chapter + 1)} title="下一章（→）">下一章</button>
      <span className="text-xs text-muted">{chapter + 1}/{info.chapters.length}</span>
      <span className="ml-auto" />
      {look.controls}
    </div>
    <iframe ref={frame} key={chapter} onLoad={onLoad} className="h-[calc(100vh-11rem)] min-h-80 w-full rounded-lg border border-line" title={info.chapters[chapter].title}
      sandbox="allow-same-origin" src={info.asset_base + info.chapters[chapter].entry.split('/').map(encodeURIComponent).join('/')} />
  </div>;
}

type Chunk = { lines: string[]; start_line: number; start_offset: number; total_lines: number; next_offset: number; has_more: boolean };

// TXT：按 300 行一段读取，进度记行号与字节偏移（与桌面端、旧网页端同一字段）。
function TxtReader({ book, saved, save }: { book: Novel; saved: Saved; save(body: Record<string, number>): void }) {
  const [cursor, setCursor] = useState({ line: saved.position?.start_line ?? 1, offset: saved.position?.byte_offset ?? 0 });
  const [chunk, setChunk] = useState<Chunk>();
  const [history, setHistory] = useState<{ line: number; offset: number }[]>([]);
  const [error, setError] = useState('');
  const look = useLook();
  const body = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const controller = new AbortController();
    api<Chunk>(`/api/media/text/chunk?path=${encodeURIComponent(book.id)}&start=${cursor.line}&limit=300${cursor.offset >= 0 ? `&offset=${cursor.offset}` : ''}`, { signal: controller.signal })
      .then(next => {
        setChunk(next); setError('');
        body.current?.scrollTo(0, 0);
        save({ start_line: next.start_line, byte_offset: next.start_offset });
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [book.id, cursor]); // eslint-disable-line react-hooks/exhaustive-deps
  const prev = () => { setCursor(history.at(-1) ?? { line: Math.max(1, cursor.line - 300), offset: -1 }); setHistory(h => h.slice(0, -1)); };
  const next = () => { if (!chunk?.has_more) return; setHistory(h => [...h.slice(-255), { line: chunk.start_line, offset: chunk.start_offset }]); setCursor({ line: chunk.start_line + chunk.lines.length, offset: chunk.next_offset }); };
  return <div className="space-y-2">
    <div className="flex flex-wrap items-center gap-2">
      <button className={button} disabled={cursor.line <= 1} onClick={prev}>上一段</button>
      <button className={button} disabled={!chunk?.has_more} onClick={next}>下一段</button>
      {chunk ? <span className="text-xs text-muted">第 {chunk.start_line} 行{chunk.total_lines ? ` / 共 ${chunk.total_lines} 行` : ''}</span> : null}
      <span className="ml-auto" />
      {look.controls}
    </div>
    {error ? <p className="text-danger">{error}</p> : null}
    <div ref={body} className={`h-[calc(100vh-11rem)] min-h-80 overflow-y-auto rounded-lg border border-line ${look.night ? 'bg-[#0d1117] text-[#c9d1d9]' : 'bg-[#fbf8f1] text-[#222]'}`}>
      <p className="mx-auto max-w-[46em] whitespace-pre-wrap break-words px-5 py-4 leading-[1.8]" style={{ fontSize: look.size }}>{chunk?.lines.join('\n') ?? '正在读取…'}</p>
      {chunk?.has_more ? <div className="pb-6 text-center"><button className={button} onClick={next}>下一段</button></div> : null}
    </div>
  </div>;
}

// ---------- 文档 ----------
type DocEntry = { name: string; path: string; is_dir: boolean; size?: number; library?: string };

const docName = (p: string) => p.slice(p.lastIndexOf('/') + 1);
// 每篇文档有可分享的地址 #text/doc/<逻辑路径>。
const docPrefix = '#text/doc/';
const linkedDoc = () => window.location.hash.startsWith(docPrefix) ? decodeURIComponent(window.location.hash.slice(docPrefix.length)) : '';
const parentDir = (p: string) => { const parent = p.slice(0, p.lastIndexOf('/')); return parent.endsWith(':media_library/docs') ? '' : parent; };
// 逻辑路径「库ID:media_library/docs/a/b.md」上解析相对链接；越出 docs 根目录的返回空。
function resolveDocLink(from: string, href: string) {
  const colon = from.indexOf(':');
  const lib = from.slice(0, colon), rel = from.slice(colon + 1);
  const target = decodeURIComponent(new URL(href.split('#')[0], `http://x/${rel.split('/').map(encodeURIComponent).join('/')}`).pathname.slice(1));
  return target.startsWith('media_library/docs/') ? `${lib}:${target}` : '';
}

function Docs() {
  const [doc, setDoc] = useState(() => linkedDoc() || stored('omnihub.docs.open', ''));
  const [dir, setDir] = useState(() => linkedDoc() ? parentDir(linkedDoc()) : stored('omnihub.docs.dir', ''));
  const [entries, setEntries] = useState<DocEntry[]>([]);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('');
  useEffect(() => store('omnihub.docs.dir', dir), [dir]);
  useEffect(() => {
    store('omnihub.docs.open', doc);
    const hash = doc ? docPrefix + encodeURIComponent(doc) : '#text';
    if (window.location.hash !== hash) window.history.replaceState(null, '', hash);
  }, [doc]);
  useEffect(() => {
    const controller = new AbortController();
    setError('');
    api<DocEntry[]>(`/api/media/text/docs${dir ? `?dir=${encodeURIComponent(dir)}` : ''}`, { signal: controller.signal })
      .then(setEntries).catch((e: Error) => { if (!controller.signal.aborted) { setEntries([]); setError(e.message); if (dir) setDir(''); } });
    return () => controller.abort();
  }, [dir]);
  const files = entries.filter(e => !e.is_dir);
  // 打开文档时，左侧目录切到它所在的目录，便于上一篇/下一篇。
  const open = (path: string) => { setDoc(path); setDir(parentDir(path)); };
  const index = files.findIndex(f => f.path === doc);
  const crumbs = dir ? dir.slice(dir.indexOf(':') + 1).split('/').slice(2) : [];
  const shown = filter ? entries.filter(e => e.name.toLowerCase().includes(filter.toLowerCase())) : entries;
  return <div className={`grid gap-3 ${doc ? 'md:grid-cols-[16rem_1fr]' : ''}`}>
    <aside className={`min-w-0 space-y-1 ${doc ? 'hidden md:block md:sticky md:top-14 md:max-h-[calc(100vh-5rem)] md:overflow-y-auto md:overflow-x-hidden' : ''}`}>
      <div className="flex flex-wrap items-center gap-1 break-all text-sm">
        <button className="text-primary hover:underline" onClick={() => setDir('')}>文档</button>
        {crumbs.map((c, i) => <span key={i} className="flex items-center gap-1"><span className="text-muted">/</span>
          <button className="text-primary hover:underline" onClick={() => setDir(dir.slice(0, dir.indexOf(':') + 1) + 'media_library/docs/' + crumbs.slice(0, i + 1).join('/'))}>{c}</button></span>)}
      </div>
      <input className={`${select} w-full`} placeholder="筛选当前目录" value={filter} onChange={e => setFilter(e.target.value)} />
      {error ? <p className="text-sm text-danger">{error}</p> : null}
      {!error && !entries.length ? <p className="text-sm text-muted">这里没有文档。</p> : null}
      <div className={doc ? '' : 'grid gap-1 sm:grid-cols-2 lg:grid-cols-3'}>
        {shown.map(e => <button key={e.path} className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm ${e.path === doc ? 'bg-panel text-primary' : 'hover:bg-panel'}`}
          onClick={() => { setFilter(''); if (e.is_dir) setDir(e.path); else setDoc(e.path); }}>
          <span className="shrink-0">{e.is_dir ? '📁' : /\.rst$/i.test(e.name) ? '📘' : '📄'}</span><span className="min-w-0 truncate">{e.name}</span>
        </button>)}
      </div>
    </aside>
    {doc ? <DocReader key={doc} path={doc} onOpen={open} onClose={() => setDoc('')}
      prev={index > 0 ? files[index - 1].path : undefined} next={index >= 0 && index < files.length - 1 ? files[index + 1].path : undefined} /> : null}
  </div>;
}

function DocReader({ path, onOpen, onClose, prev, next }: { path: string; onOpen(path: string): void; onClose(): void; prev?: string; next?: string }) {
  const [html, setHtml] = useState<string>();
  const [error, setError] = useState('');
  const article = useRef<HTMLElement>(null);
  useEffect(() => {
    const controller = new AbortController();
    request(`/api/media/text/raw?path=${encodeURIComponent(path)}`, { signal: controller.signal })
      .then(r => r.text())
      .then(async text => {
        const render = await import('./docs/render');
        const lower = path.toLowerCase();
        const out = lower.endsWith('.rst') ? await render.renderRstDoc(text) : lower.endsWith('.txt') ? `<pre>${render.escapeHtml(text)}</pre>` : await render.renderMarkdownDoc(text);
        if (!controller.signal.aborted) setHtml(out);
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [path]);
  useEffect(() => {
    if (html === undefined || !article.current) return;
    window.scrollTo(0, 0);
    void import('./docs/render').then(r => article.current && r.drawDiagrams(article.current));
  }, [html]);
  const onClick = (e: React.MouseEvent) => {
    const anchor = (e.target as HTMLElement).closest('a[href^="#"]:not([data-doc])');
    if (anchor) {   // 文内锚点：不改地址栏的 hash（那是页面路由），直接滚过去
      e.preventDefault();
      const id = decodeURIComponent(anchor.getAttribute('href')!.slice(1));
      [...article.current!.querySelectorAll('[id]')].find(el => el.id === id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      return;
    }
    const a = (e.target as HTMLElement).closest('a[data-doc]');
    if (!a) return;
    e.preventDefault();
    const target = resolveDocLink(path, a.getAttribute('data-doc') ?? '');
    if (target) onOpen(target); else setError('链接指向文档目录以外，无法打开');
  };
  const nav = <div className="flex flex-wrap items-center gap-2">
    <button className={button} onClick={onClose}>← 目录</button>
    <button className={button} disabled={!prev} onClick={() => prev && onOpen(prev)} title={prev ? docName(prev) : ''}>上一篇</button>
    <button className={button} disabled={!next} onClick={() => next && onOpen(next)} title={next ? docName(next) : ''}>下一篇</button>
  </div>;
  return <div className="min-w-0 space-y-3">
    <div className="flex flex-wrap items-center gap-2">{nav}<h2 className="min-w-0 flex-1 truncate font-semibold">{docName(path)}</h2></div>
    {error ? <p className="text-danger">{error}</p> : null}
    {html === undefined && !error ? <p role="status">正在渲染…</p> : null}
    {html !== undefined ? <article ref={article} className="doc-body" onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} /> : null}
    {html !== undefined ? nav : null}
  </div>;
}
