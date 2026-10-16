import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';
import ChannelBar from './ChannelBar';
import { canPlayCodec } from './codecs';
import { feedPath, feedQuery, newSeed, sendEvent, trash, useGroups, useLiked } from './feed';
import { button, chip, iconButton, store, stored } from './ui';

const Matrix = lazy(() => import('./Matrix'));

type Post = { id: string; creator_id: string; kind: 'video' | 'album'; title: string; published: string; image_count?: number; music?: string; library_id: string; has_thumbnail: boolean; codec?: string; liked?: boolean };
const query = (post: Post, extra: Record<string, string> = {}) => new URLSearchParams({ library_id: post.library_id, id: post.id, ...extra }).toString();

// 短视频：频道（推荐 | 我的点赞 | 平台 | 📌 固定的博主 | 其余博主）+ 作品网格 + 查看器；也可切到多联放映。
// 推荐、点赞、回收站、行为上报与桌面端共用 /api/feed/shortvideo。
export default function ShortVideoPage({ active }: { active: boolean }) {
  const [mode, setMode] = useState<'browse' | 'matrix'>(() => window.location.hash.endsWith('/matrix') ? 'matrix' : stored('omnihub.sv.mode', 'browse'));
  const [channel, setChannel] = useState(() => stored('omnihub.sv.channel', 'recommend'));
  const [seed, setSeed] = useState(newSeed);
  const [open, setOpen] = useState<number>();
  const [reload, setReload] = useState(0);
  useEffect(() => store('omnihub.sv.mode', mode), [mode]);
  useEffect(() => store('omnihub.sv.channel', channel), [channel]);
  const { groups, pins, setPinned } = useGroups('shortvideo', active);
  const liked = useLiked('shortvideo');
  const platforms = [...new Set(groups.map(g => g.platform).filter(Boolean))].map(p => ({ channel: `tag:platform:${p}`, label: p! }));
  const browsing = active && mode === 'browse';
  const endpoint = browsing ? `${feedPath('shortvideo', 'items')}?${feedQuery({ channel, seed, r: reload || undefined })}` : '';
  const paged = usePaged<Post>(endpoint);
  const group = channel.startsWith('group:') ? channel.slice(6) : '';
  // 列表变短（移到回收站后）时，查看器停在原位置看下一个；原来就是最后一个则退到前一个。
  useEffect(() => { if (open !== undefined && paged.total > 0 && open >= paged.total) setOpen(paged.total - 1); }, [open, paged.total]);

  if (!active) return null;
  if (mode === 'matrix') return <Suspense fallback={<p role="status">正在加载…</p>}><Matrix domain="shortvideo" onExit={() => setMode('browse')} /></Suspense>;
  return <section className="space-y-1.5">
    <div className="flex flex-wrap items-center gap-1.5">
      <button className={chip(true)}>浏览</button>
      <button className={chip(false)} onClick={() => setMode('matrix')}>多联放映</button>
      <span className="ml-auto" />
      {channel === 'recommend' ? <button className={chip(false)} onClick={() => setSeed(newSeed())}>换一批</button> : null}
      {group ? <button className={chip(pins.includes(group))} onClick={() => setPinned(group, !pins.includes(group))}>{pins.includes(group) ? '取消固定博主' : '📌 固定这个博主'}</button> : null}
      <span className={`text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 个作品`}</span>
    </div>
    <ChannelBar groups={groups} pins={pins} channel={channel} onPick={setChannel} filters={platforms} />
    <VirtualGrid total={paged.total} minColumnWidth={170} aspect={16 / 9} extraHeight={44} render={index => {
      const post = paged.item(index);
      if (!post) return <div className="h-full rounded-lg border border-line bg-panel" />;
      return <button className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary" onClick={() => setOpen(index)}>
        <div className="relative min-h-0 flex-1 bg-background">
          {post.has_thumbnail
            ? <img className="h-full w-full object-cover" src={`/api/media/shortvideo/thumbnail?${query(post)}`} alt="" decoding="async" />
            : <span className="flex h-full items-center justify-center text-4xl text-line">{post.kind === 'album' ? '🖼' : '▶'}</span>}
          <span className="absolute right-1 top-1 rounded bg-black/60 px-1.5 text-xs text-white">{post.kind === 'album' ? `图集 ${post.image_count}` : '视频'}</span>
          {liked.isLiked(post) ? <span className="absolute left-1 top-1 text-[#f778ba]">♥</span> : null}
        </div>
        <div className="h-11 px-1.5 py-1"><p className="truncate text-xs">{post.title}</p><p className="truncate text-xs text-muted">{post.creator_id.split('/')[1]} · {post.published}</p></div>
      </button>;
    }} />
    {open !== undefined ? <Viewer post={paged.item(open)} liked={liked.isLiked(paged.item(open))} onLike={() => { const p = paged.item(open); if (p) liked.toggle(p); }}
      onTrash={() => { const p = paged.item(open); return p ? trash('shortvideo', p.id).then(() => setReload(n => n + 1)) : Promise.resolve(); }}
      onClose={() => setOpen(undefined)}
      onPrev={open > 0 ? () => setOpen(open - 1) : undefined}
      onNext={open < paged.total - 1 ? () => setOpen(open + 1) : undefined} /> : null}
  </section>;
}

// Viewer：全屏播放视频或翻看图集。←/→ 切作品，↑/↓ 翻图，Z 点赞，Delete 按两次移到回收站，Esc 关闭。
// 行为上报：展示即 view；离开时 2 秒内划走算 skip，看完（或 80%）算 complete，停留 8 秒以上算 dwell。
function Viewer({ post, liked, onLike, onTrash, onClose, onPrev, onNext }: {
  post?: Post; liked: boolean; onLike(): void; onTrash(): Promise<void>; onClose(): void; onPrev?: () => void; onNext?: () => void;
}) {
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState('');
  const [armed, setArmed] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const shown = useRef<{ post: Post; at: number; pages: number } | undefined>(undefined);
  const pages = post?.image_count ?? 0;
  const finish = () => {
    const s = shown.current;
    if (!s) return;
    const ms = Date.now() - s.at, v = video.current;
    const type = s.post.kind === 'video' && v && v.duration > 0 && v.currentTime >= v.duration * 0.8 ? 'complete'
      : ms < 2000 ? 'skip' : ms >= 8000 || s.pages >= 3 ? 'dwell' : '';
    if (type) sendEvent('shortvideo', s.post.id, type);
    shown.current = undefined;
  };
  useEffect(() => {
    setPage(1);
    finish();
    if (post) { shown.current = { post, at: Date.now(), pages: 1 }; sendEvent('shortvideo', post.id, 'view'); }
  }, [post?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => finish(), []); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (shown.current) shown.current.pages = Math.max(shown.current.pages, page); }, [page]);
  useEffect(() => { if (!notice) return; const t = window.setTimeout(() => setNotice(''), 2500); return () => window.clearTimeout(t); }, [notice]);
  useEffect(() => { if (!armed) return; const t = window.setTimeout(() => setArmed(false), 3000); return () => window.clearTimeout(t); }, [armed]);
  const doTrash = () => {
    if (!post) return;
    if (!armed) { setArmed(true); setNotice('再点一次移到回收站'); return; }
    setArmed(false); shown.current = undefined;
    video.current?.removeAttribute('src');
    onTrash().then(() => setNotice('已移到系统回收站')).catch((e: Error) => setNotice(e.message));
  };
  const keys = useRef({ onClose, onPrev, onNext, pages, onLike, doTrash });
  keys.current = { onClose, onPrev, onNext, pages, onLike, doTrash };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const k = keys.current;
      if (e.key === 'Escape') k.onClose();
      else if (e.key === 'ArrowLeft') k.onPrev?.();
      else if (e.key === 'ArrowRight') k.onNext?.();
      else if (e.key === 'ArrowDown') setPage(p => Math.min(k.pages, p + 1));
      else if (e.key === 'ArrowUp') setPage(p => Math.max(1, p - 1));
      else if (e.key === 'z' || e.key === 'Z') k.onLike();
      else if (e.key === 'Delete') k.doTrash();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  return <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex flex-col bg-black/95 text-white">
    <div className="flex flex-wrap items-center gap-2 p-2">
      <p className="min-w-0 flex-1 truncate text-sm">{post ? `${post.creator_id.split('/')[1]} · ${post.published} · ${post.title}` : '加载中…'}</p>
      {notice ? <span className={`text-sm ${armed ? 'font-bold text-[#ff7b72]' : 'text-[#3fb950]'}`}>{notice}</span> : null}
      <button className={iconButton} onClick={onLike} title="点赞（Z）">{liked ? <span className="text-[#f778ba]">♥</span> : '♡'}</button>
      <button className={`${iconButton} ${armed ? 'bg-white/20' : ''}`} onClick={doTrash} title="移到系统回收站（Delete 按两次）">🗑</button>
      <button className={button} disabled={!onPrev} onClick={onPrev}>上一个</button>
      <button className={button} disabled={!onNext} onClick={onNext}>下一个</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div>
    <div className="relative flex min-h-0 flex-1 items-center justify-center">
      {!post ? <p>加载中…</p> : post.kind === 'video' && !canPlayCodec(post.codec)
        ? <p className="max-w-md text-center">这个视频是 {post.codec?.toUpperCase()} 编码，当前浏览器无法解码。请在 Deck 上的 omniHub 客户端中播放。</p>
        : post.kind === 'video'
        ? <video ref={video} key={post.id} className="max-h-full max-w-full" src={`/api/media/shortvideo/stream?${query(post)}`} controls autoPlay playsInline
            onEnded={() => { sendEvent('shortvideo', post.id, 'complete'); shown.current = undefined; onNext?.(); }} />
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
