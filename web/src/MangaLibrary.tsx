import { useEffect, useMemo, useState } from 'react';
import { api, post, request } from './api';
import VirtualGrid from './VirtualGrid';
import { chip, divider, explain, notify, select, store, stored } from './ui';
import { openManga, type Book } from './MangaReader';

type Recent = { title: string; logical_path: string; page_index: number };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

// MangaLibrary：书架一次取全（几百本，元数据很小，本页会话内缓存），在本地搜索与筛选：
// 书名/作者/标签关键字、标签栏（出现最多的标签）、♥ 我的点赞、连载未完；卡片上的标签点一下即按它筛选。
// 阅读器只挂载选中的那本，图片只加载可视区附近。
type ShelfBook = Book & { author?: string; tags?: string[] | null; is_complete?: boolean };
let shelfCache: ShelfBook[] | undefined;

// 漫画按子目录分级：manga/standard/（一般）与 manga/nsfw/；旧布局直接放在 manga/ 下的算 NSFW（服务端会自动搬进 nsfw/）。
// 没有权限的子目录服务端不列出，所以按钮只出现在能看到的分类上。
const folderOf = (logical: string) => {
  const rest = logical.slice(logical.indexOf('media_library/manga/') + 'media_library/manga/'.length);
  return rest.includes('/') ? rest.slice(0, rest.indexOf('/')) : 'nsfw';
};
const folderNames: Record<string, string> = { standard: '一般', nsfw: 'NSFW' };

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
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  // 阅读器关掉后刷新「继续阅读」和封面（可能换了封面）
  const [closed, setClosed] = useState(0);
  useEffect(() => {
    const done = () => { setClosed(c => c + 1); setRevision(v => v + 1); };
    globalThis.addEventListener('omnihub-manga-closed', done);
    return () => globalThis.removeEventListener('omnihub-manga-closed', done);
  }, []);
  const [recent, setRecent] = useState<Recent[]>([]);
  const [likes, setLikes] = useState<Set<string>>(new Set());
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState(() => stored('omnihub.manga.filter', ''));   // '' 全部 | '__liked' | '__incomplete' | 标签
  useEffect(() => store('omnihub.manga.filter', filter), [filter]);
  const [folder, setFolder] = useState(() => stored('omnihub.manga.folder', ''));   // '' 全部 | standard | nsfw | 其他子目录
  useEffect(() => store('omnihub.manga.folder', folder), [folder]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Recent[]>('/api/media/manga/recent', { signal: controller.signal }).then(setRecent).catch(() => undefined);
    api<string[]>('/api/media/manga/likes', { signal: controller.signal }).then(list => setLikes(new Set(list))).catch(() => undefined);
    return () => controller.abort();
  }, [active, closed]);
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
  const shelf = books ?? [];
  const folders = useMemo(() => {
    const counts = new Map<string, number>();
    for (const b of shelf) counts.set(folderOf(b.logical_path), (counts.get(folderOf(b.logical_path)) ?? 0) + 1);
    return [...counts].sort((a, b) => (a[0] === 'standard' ? -1 : b[0] === 'standard' ? 1 : a[0].localeCompare(b[0])));
  }, [shelf]);
  // 所选分类没有了（权限变了、库拔了）就回到全部
  const all = folder && folders.some(([f]) => f === folder) ? shelf.filter(b => folderOf(b.logical_path) === folder) : shelf;
  const tagCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const b of all) for (const t of b.tags ?? []) counts.set(t, (counts.get(t) ?? 0) + 1);
    return [...counts].sort((a, b) => b[1] - a[1]).slice(0, 30);
  }, [all]);
  const needle = query.trim().toLowerCase();
  const shown = all.filter(b => (filter === '' || filter === '__recent' ? true : filter === '__liked' ? likes.has(b.logical_path) : filter === '__incomplete' ? b.is_complete === false : (b.tags ?? []).includes(filter))
    && (!needle || b.title.toLowerCase().includes(needle) || (b.author ?? '').toLowerCase().includes(needle) || (b.tags ?? []).some(t => t.toLowerCase().includes(needle))));
  const incomplete = all.filter(b => b.is_complete === false).length;
  // 分类（一般 / NSFW）与文本页的小说/文档一样放在工具条最前面，用竖线隔开；再点一次已选的分类回到全部。
  const shownRecent = recent.filter(r => !folder || folderOf(r.logical_path) === folder);
  return <section className="space-y-2">
    <div className="flex flex-wrap items-center gap-1.5">
      {folders.length > 1 ? <>
        {folders.map(([f, n]) => <button key={f} className={chip(folder === f)} title={folder === f ? '再点一次显示全部分类' : undefined} onClick={() => setFolder(folder === f ? '' : f)}>{folderNames[f] ?? f} <span className="text-xs">{n}</span></button>)}
        <span className={divider} />
      </> : null}
      {recent.length ? <button className={chip(filter === '__recent')} onClick={() => setFilter('__recent')}>继续阅读 {shownRecent.length}</button> : null}
      <button className={chip(filter === '')} onClick={() => setFilter('')}>全部 {all.length}</button>
      <button className={chip(filter === '__liked')} onClick={() => setFilter('__liked')}>♥ 我的点赞 {likes.size}</button>
      {incomplete ? <button className={chip(filter === '__incomplete')} onClick={() => setFilter('__incomplete')}>连载/未完 {incomplete}</button> : null}
      <span className={divider} />
      {/* 标签条与上面同一行（窄屏时单独一行） */}
      <div className="order-last min-w-0 basis-full sm:order-none sm:flex-1 sm:basis-0"><div className="flex gap-1.5 overflow-x-auto pb-0.5">
        {tagCounts.map(([t, n]) => <button key={t} className={chip(filter === t)} onClick={() => setFilter(filter === t ? '' : t)}>🏷 {t} <span className="text-xs">{n}</span></button>)}
        {filter && !['__liked', '__incomplete', '__recent'].includes(filter) && !tagCounts.some(([t]) => t === filter) ? <button className={chip(true)} onClick={() => setFilter('')}>🏷 {filter} ✕</button> : null}
      </div></div>
      <input className={`${select} w-44`} placeholder="搜索书名、作者、标签" value={query} onChange={e => setQuery(e.target.value)} />
      <button className={button} onClick={() => setRevision(value => value + 1)}>刷新</button>
    </div>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    {!books && !error ? <p role="status">正在读取书架…</p> : null}
    {filter === '__recent' ? <div className="space-y-1">{!shownRecent.length ? <p className="text-sm text-muted">这个分类里还没有读过的漫画。</p> : null}
      <div className="flex flex-wrap gap-2">{shownRecent.map(r => <button key={r.logical_path} className="w-32 shrink-0 overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary"
        onClick={() => openManga({ title: r.title, logical_path: r.logical_path, library_name: '', read_only: true, total_pages: 0 })}>
        <img className="aspect-[3/4] w-full object-cover" loading="lazy" decoding="async" src={`/api/media/manga/cover?path=${encodeURIComponent(r.logical_path)}&revision=${revision}`} alt="" />
        <span className="block truncate px-1.5 pt-1 text-xs">{r.title}</span><span className="block px-1.5 pb-1 text-xs text-muted">第 {r.page_index + 1} 页</span>
      </button>)}</div></div> : null}
    {books && filter !== '__recent' && (filter || needle) ? <p className="text-xs text-muted">{shown.length} 本</p> : null}
    {filter === '__recent' ? null : <VirtualGrid total={shown.length} minColumnWidth={150} aspect={3 / 4} extraHeight={64} render={i => {
      const book = shown[i];
      const liked = likes.has(book.logical_path);
      return <div className="relative flex h-full flex-col overflow-hidden rounded-lg border border-line bg-panel hover:border-primary">
        <button className="relative min-h-0 flex-1" onClick={() => openManga(book)} title={book.title}>
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
    }} />}
    {books && !shown.length ? <p className="text-muted">{filter === '__liked' ? '还没有点赞的漫画。' : '没有匹配的漫画。'}</p> : null}
  </section>;
}
