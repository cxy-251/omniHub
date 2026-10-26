import { lazy, Suspense, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';
import ChannelBar from './ChannelBar';
import { canPlayCodec, webCodecs } from './codecs';
import { feedPath, feedQuery, newSeed, sendEvent, trash, useGroups, useLiked } from './feed';
import { button, chip, divider, iconButton, select, store, stored } from './ui';

const Matrix = lazy(() => import('./Matrix'));

type Post = { id: string; creator_id: string; kind: 'video' | 'album'; title: string; published: string; image_count?: number; music?: string; library_id: string; has_thumbnail: boolean; codec?: string; liked?: boolean };
const query = (post: Post, extra: Record<string, string> = {}) => new URLSearchParams({ library_id: post.library_id, id: post.id, ...extra }).toString();

// 短视频：先选平台（全部 | 抖音 | 快手 | … | YouTube），再选频道（推荐 | 我的点赞 | 📌 固定的博主 | 其余博主）+ 作品网格 + 查看器；也可切到多联放映。
// 推荐、点赞、回收站、行为上报与桌面端共用 /api/feed/shortvideo。网页端不展示 HEVC 视频（codecs 过滤，图集不受影响）。
export default function ShortVideoPage({ active }: { active: boolean }) {
  const [mode, setMode] = useState<'browse' | 'matrix'>(() => window.location.hash.endsWith('/matrix') ? 'matrix' : stored('omnihub.sv.mode', 'browse'));
  // 旧版把平台当频道（tag:platform:抖音）存着：改成选中该平台、频道回到推荐。
  const legacy = stored('omnihub.sv.channel', 'recommend');
  const [channel, setChannel] = useState(() => legacy.startsWith('tag:platform:') ? 'recommend' : legacy);
  const [platform, setPlatform] = useState(() => legacy.startsWith('tag:platform:') ? legacy.slice(13) : stored('omnihub.sv.platform', ''));
  const [seed, setSeed] = useState(newSeed);
  const [open, setOpen] = useState<number>();
  const [reload, setReload] = useState(0);
  useEffect(() => store('omnihub.sv.mode', mode), [mode]);
  useEffect(() => store('omnihub.sv.channel', channel), [channel]);
  useEffect(() => store('omnihub.sv.platform', platform), [platform]);
  const { groups, pins, setPinned } = useGroups('shortvideo', active);
  const liked = useLiked('shortvideo');
  const platforms = [...new Set(groups.map(g => g.platform).filter(Boolean))] as string[];
  const shownGroups = platform ? groups.filter(g => g.platform === platform) : groups;
  // 平台没有了（库离线等）就回到「全部」；当前博主不属于所选平台时回到推荐。
  useEffect(() => { if (platform && groups.length && !platforms.includes(platform)) setPlatform(''); }, [platform, groups]); // eslint-disable-line react-hooks/exhaustive-deps
  const pickPlatform = (p: string) => { setPlatform(p); if (channel.startsWith('group:') && !groups.some(g => `group:${g.id}` === channel && (!p || g.platform === p))) setChannel('recommend'); };
  const browsing = active && mode === 'browse';
  const endpoint = browsing ? `${feedPath('shortvideo', 'items')}?${feedQuery({ channel, scope: platform ? `tag:platform:${platform}` : undefined, seed, r: reload || undefined, codecs: webCodecs() })}` : '';
  const paged = usePaged<Post>(endpoint);
  const group = channel.startsWith('group:') ? channel.slice(6) : '';
  // 列表变短（移到回收站后）时，查看器停在原位置看下一个；原来就是最后一个则退到前一个。
  useEffect(() => { if (open !== undefined && paged.total > 0 && open >= paged.total) setOpen(paged.total - 1); }, [open, paged.total]);

  if (!active) return null;
  if (mode === 'matrix') return <Suspense fallback={<p role="status">正在加载…</p>}><Matrix domain="shortvideo" onExit={() => setMode('browse')} /></Suspense>;
  return <section className="space-y-1.5">
    {/* 一行：浏览 | 多联放映 ‖ 平台 ▾ ‖ 频道条（窄屏时单独一行）‖ 换一批 · 固定 · 作品数 */}
    <div className="flex flex-wrap items-center gap-1.5">
      <button className={chip(true)}>浏览</button>
      <button className={chip(false)} onClick={() => setMode('matrix')}>多联放映</button>
      <span className={divider} />
      {platforms.length > 1 ? <select aria-label="平台" className={select} value={platform} onChange={e => pickPlatform(e.target.value)}>
        {['', ...platforms].map(p => <option key={p || 'all'} value={p}>{p || '全部平台'}</option>)}
      </select> : null}
      <div className="order-last min-w-0 basis-full sm:order-none sm:flex-1 sm:basis-0">
        <ChannelBar groups={shownGroups} pins={pins.filter(id => shownGroups.some(g => g.id === id))} channel={channel} onPick={setChannel} />
      </div>
      <span className={`${divider} hidden sm:block`} />
      {channel === 'recommend' ? <button className={chip(false)} onClick={() => setSeed(newSeed())}>换一批</button> : null}
      {group ? <button className={chip(pins.includes(group))} onClick={() => setPinned(group, !pins.includes(group))} title={pins.includes(group) ? '取消固定这个博主' : '把这个博主固定在频道条前面'}>{pins.includes(group) ? '取消固定' : '📌 固定'}</button> : null}
      <span className={`text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 个`}</span>
    </div>
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
        <div className="h-11 px-1.5 py-1"><p className="truncate text-xs">{post.title}</p><p className="truncate text-xs text-muted">{post.creator_id.split('/').slice(1).join(' / ')} · {post.published}</p></div>
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
  // 静音和音量跨视频记住（存在浏览器里）：在一个视频上点了静音，后面的视频也静音
  const [sound, setSound] = useState(() => stored('omnihub.sv.sound', { muted: false, volume: 1 }));
  useEffect(() => store('omnihub.sv.sound', sound), [sound]);
  const applySound = (el: HTMLMediaElement | null) => { if (el) { el.muted = sound.muted; el.volume = sound.volume; } };
  const keepSound = (el: HTMLMediaElement) => setSound({ muted: el.muted, volume: el.volume });
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
  // 点画面：左边三分之一上一个、右边三分之一下一个（图集先翻图片，翻到头再换作品），中间暂停/继续（图集翻下一张）。
  // 滑动：左滑下一个、右滑上一个、下滑返回。视频底部留出原生进度条，不盖住。
  const press = useRef<{ x: number; y: number } | undefined>(undefined);
  const prevPage = () => { if (page > 1) setPage(page - 1); else onPrev?.(); };
  const nextPage = () => { if (page < pages) setPage(page + 1); else onNext?.(); };
  function release(e: ReactPointerEvent<HTMLDivElement>) {
    const start = press.current;
    press.current = undefined;
    if (!start || !post) return;
    const dx = e.clientX - start.x, dy = e.clientY - start.y;
    if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.2) { if (dx < 0) onNext?.(); else onPrev?.(); return; }
    if (dy > 90 && dy > Math.abs(dx) * 1.2) { onClose(); return; }
    if (Math.abs(dx) > 12 || Math.abs(dy) > 12) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const isVideo = post.kind === 'video';
    if (x < 1 / 3) { if (isVideo) onPrev?.(); else prevPage(); }
    else if (x > 2 / 3) { if (isVideo) onNext?.(); else nextPage(); }
    else if (isVideo) { const v = video.current; if (v) { if (v.paused) void v.play(); else v.pause(); } }
    else nextPage();
  }
  const gestures = <div className={`absolute inset-x-0 top-0 z-10 touch-none ${post?.kind === 'video' ? 'bottom-16' : 'bottom-14'}`}
    onPointerDown={e => { press.current = { x: e.clientX, y: e.clientY }; }} onPointerUp={release} onPointerCancel={() => { press.current = undefined; }} />;
  return <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex flex-col bg-black/95 text-white">
    <div className="flex flex-wrap items-center gap-2 p-2">
      <p className="min-w-0 flex-1 truncate text-sm">{post ? `${post.creator_id.split('/').slice(1).join(' / ')} · ${post.published} · ${post.title}` : '加载中…'}</p>
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
        ? <>{gestures}<video ref={el => { video.current = el; applySound(el); }} key={post.id} className="max-h-full max-w-full" src={`/api/media/shortvideo/stream?${query(post)}`} controls autoPlay playsInline
            onVolumeChange={e => keepSound(e.currentTarget)}
            onEnded={() => { sendEvent('shortvideo', post.id, 'complete'); shown.current = undefined; onNext?.(); }} /></>
        : <>
          {gestures}
          <img key={post.id + page} className="max-h-full max-w-full object-contain" src={`/api/media/shortvideo/album/image?${query(post, { index: String(page) })}`} alt={`${page}/${pages}`} />
          {post.music ? <audio key={post.id} ref={applySound} onVolumeChange={e => keepSound(e.currentTarget)} src={`/api/media/shortvideo/stream?${query(post, { music: '1' })}`} autoPlay loop controls className="absolute bottom-3 left-3 z-20 h-8" /> : null}
          <div className="absolute bottom-3 right-3 z-20 flex items-center gap-2">
            <button className={button} disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一张</button>
            <span className="text-sm">{page} / {pages}</span>
            <button className={button} disabled={page >= pages} onClick={() => setPage(p => p + 1)}>下一张</button>
          </div>
        </>}
    </div>
  </div>;
}
