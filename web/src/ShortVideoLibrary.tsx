import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';

const VideoMatrix = lazy(() => import('./VideoMatrix'));

type Creator = { id: string; platform: string; name: string; posts: number };
type Post = { id: string; creator_id: string; kind: 'video' | 'album'; title: string; published: string; image_count?: number; music?: string; library_id: string; has_thumbnail: boolean };

const chip = (on: boolean) => `shrink-0 rounded-full border px-3 py-1 text-sm ${on ? 'border-primary text-primary' : 'border-line text-muted hover:text-foreground'}`;
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

function query(post: Post, extra: Record<string, string> = {}) {
  return new URLSearchParams({ library_id: post.library_id, id: post.id, ...extra }).toString();
}

// ShortVideoLibrary：浏览（平台 → 博主 → 作品网格 → 播放/翻看）与多联放映两种模式。
export default function ShortVideoLibrary({ active, onAccompany }: { active: boolean; onAccompany?: () => void }) {
  const [mode, setMode] = useState<'browse' | 'matrix'>('browse');
  const [creators, setCreators] = useState<Creator[]>([]);
  const [platform, setPlatform] = useState('');
  const [creator, setCreator] = useState('');
  const [error, setError] = useState('');
  const [open, setOpen] = useState<number>();

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Creator[]>('/api/media/shortvideo/creators', { signal: controller.signal })
      .then(setCreators).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [active]);

  const platforms = [...new Set(creators.map(c => c.platform))];
  const shownCreators = creators.filter(c => !platform || c.platform === platform);

  const browsing = active && mode === 'browse';
  const endpoint = creator ? `/api/media/shortvideo/posts?creator=${encodeURIComponent(creator)}`
    : platform ? `/api/media/shortvideo/posts?platform=${encodeURIComponent(platform)}` : '/api/media/shortvideo/posts';
  const paged = usePaged<Post>(browsing ? endpoint : '');

  return <section className="space-y-4">
    <div className="flex gap-2">
      <button className={chip(mode === 'browse')} onClick={() => setMode('browse')}>浏览</button>
      <button className={chip(mode === 'matrix')} onClick={() => setMode('matrix')}>多联放映</button>
    </div>
    {mode === 'matrix' ? <Suspense fallback={<p role="status">正在加载…</p>}><VideoMatrix active={active} onAccompany={onAccompany} /></Suspense> : <>
      <div className="flex gap-2 overflow-x-auto pb-1">
        <button className={chip(!platform)} onClick={() => { setPlatform(''); setCreator(''); }}>全部平台</button>
        {platforms.map(p => <button key={p} className={chip(platform === p)} onClick={() => { setPlatform(p); setCreator(''); }}>{p}</button>)}
      </div>
      <div className="flex max-h-28 flex-wrap gap-2 overflow-y-auto">
        <button className={chip(!creator)} onClick={() => setCreator('')}>全部博主</button>
        {shownCreators.map(c => <button key={c.id} className={chip(creator === c.id)} onClick={() => setCreator(c.id)}>{c.name} · {c.posts}</button>)}
      </div>
      {error || paged.error ? <p role="alert" className="text-danger">{error || paged.error}</p> : null}
      <p className="text-sm text-muted">{paged.total ? `${paged.total} 个作品` : '没有作品。资源库需要先由管理员扫描建立索引。'}</p>
      {browsing ? <VirtualGrid total={paged.total} minColumnWidth={170} aspect={16 / 9} extraHeight={56} render={index => {
        const post = paged.item(index);
        if (!post) return <div className="h-full rounded-lg border border-line bg-panel" />;
        return <button className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary" onClick={() => setOpen(index)}>
          <div className="relative min-h-0 flex-1 bg-background">
            {post.has_thumbnail
              ? <img className="h-full w-full object-cover" src={`/api/media/shortvideo/thumbnail?${query(post)}`} alt="" decoding="async" />
              : <span className="flex h-full items-center justify-center text-4xl text-line">{post.kind === 'album' ? '🖼' : '▶'}</span>}
            <span className="absolute right-1 top-1 rounded bg-black/60 px-1.5 text-xs text-white">{post.kind === 'album' ? `图集 ${post.image_count}` : '视频'}</span>
          </div>
          <div className="h-14 p-2"><p className="truncate text-sm">{post.title}</p><p className="mt-1 truncate text-xs text-muted">{post.creator_id.split('/')[1]} · {post.published}</p></div>
        </button>;
      }} /> : null}
      {open !== undefined && paged.item(open) ? <Viewer post={paged.item(open)!} onClose={() => setOpen(undefined)}
        onPrev={open > 0 ? () => setOpen(open - 1) : undefined}
        onNext={open < paged.total - 1 ? () => setOpen(open + 1) : undefined} /> : null}
    </>}
  </section>;
}

// Viewer 全屏播放视频或翻看图集（图集带背景音乐时循环播放）。←/→ 切作品，↑/↓ 翻图，Esc 关闭。
function Viewer({ post, onClose, onPrev, onNext }: { post: Post; onClose(): void; onPrev?: () => void; onNext?: () => void }) {
  const [page, setPage] = useState(1);
  const pages = post.image_count ?? 0;
  const keys = useRef({ onClose, onPrev, onNext, pages });
  keys.current = { onClose, onPrev, onNext, pages };
  useEffect(() => setPage(1), [post]);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const k = keys.current;
      if (e.key === 'Escape') k.onClose();
      else if (e.key === 'ArrowLeft') k.onPrev?.();
      else if (e.key === 'ArrowRight') k.onNext?.();
      else if (e.key === 'ArrowDown') setPage(p => Math.min(k.pages, p + 1));
      else if (e.key === 'ArrowUp') setPage(p => Math.max(1, p - 1));
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  return <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex flex-col bg-black/95 text-white">
    <div className="flex items-center gap-3 p-3">
      <p className="min-w-0 flex-1 truncate text-sm">{post.creator_id.split('/')[1]} · {post.published} · {post.title}</p>
      <button className={button} disabled={!onPrev} onClick={onPrev}>上一个</button>
      <button className={button} disabled={!onNext} onClick={onNext}>下一个</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div>
    <div className="relative flex min-h-0 flex-1 items-center justify-center">
      {post.kind === 'video'
        ? <video key={post.id} className="max-h-full max-w-full" src={`/api/media/shortvideo/stream?${query(post)}`} controls autoPlay playsInline onEnded={onNext} />
        : <>
          <img key={post.id + page} className="max-h-full max-w-full object-contain" src={`/api/media/shortvideo/album/image?${query(post, { index: String(page) })}`} alt={`${page}/${pages}`} onClick={() => setPage(p => p < pages ? p + 1 : 1)} />
          {post.music ? <audio key={post.id} src={`/api/media/shortvideo/stream?${query(post, { music: '1' })}`} autoPlay loop controls className="absolute bottom-3 left-3 h-8" /> : null}
          <div className="absolute bottom-3 right-3 flex items-center gap-2">
            <button className={button} disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一张</button>
            <span className="text-sm">{page} / {pages}</span>
            <button className={button} disabled={page >= pages} onClick={() => setPage(p => p + 1)}>下一张</button>
          </div>
        </>}
    </div>
  </div>;
}
