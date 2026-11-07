import { lazy, Suspense, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';
import ChannelBar from './ChannelBar';
import { mangaHandlesKeyboard } from './mangaState';
import { canPlayCodec, webCodecs } from './codecs';
import { feedPath, feedQuery, newSeed, sendEvent, trash, useGroups, useLiked } from './feed';
import {
  button,
  chip,
  divider,
  enterBrowserFullscreen,
  exitBrowserFullscreen,
  getViewerFullscreenPref,
  iconButton,
  select,
  setViewerFullscreenPref,
  store,
  stored,
} from './ui';

const Matrix = lazy(() => import('./Matrix'));

type Post = { id: string; creator_id: string; kind: 'video' | 'album'; title: string; published: string; image_count?: number; music?: string; library_id: string; has_thumbnail: boolean; codec?: string; width?: number; height?: number; liked?: boolean };
const query = (post: Post, extra: Record<string, string> = {}) => new URLSearchParams({ library_id: post.library_id, id: post.id, ...extra }).toString();

// 短视频：先选平台（全部 | 抖音 | 快手 | … | YouTube），再选频道（推荐 | 我的点赞 | 📌 固定的博主 | 其余博主）+ 作品网格 + 查看器；也可切到多联放映。
// 推荐、点赞、回收站、行为上报与桌面端共用 /api/feed/shortvideo。网页端不展示 HEVC 视频（codecs 过滤，图集不受影响）。
export default function ShortVideoPage({ active, isAdmin = false, loggedIn = false }: { active: boolean; isAdmin?: boolean; loggedIn?: boolean }) {
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
  const hasPortraitPlatform = platforms.some(p => p !== 'MV');
  const canMatrix = hasPortraitPlatform && platform !== 'MV' && !group.startsWith('MV/');
  useEffect(() => { if (mode === 'matrix' && groups.length > 0 && !hasPortraitPlatform) setMode('browse'); }, [mode, groups.length, hasPortraitPlatform]);
  const isLandscapeGrid = platform === 'MV' || group.startsWith('MV/') || (() => {
    let land = 0, port = 0;
    for (let i = 0; i < Math.min(paged.total, 12); i++) {
      const it = paged.item(i);
      if (it?.width && it?.height) { if (it.width > it.height) land++; else port++; }
    }
    return land > 0 && land >= port * 2;
  })();
  // 列表变短（移到回收站后）时，查看器停在原位置看下一个；原来就是最后一个则退到前一个。
  useEffect(() => { if (open !== undefined && paged.total > 0 && open >= paged.total) setOpen(paged.total - 1); }, [open, paged.total]);

  if (!active) return null;
  if (mode === 'matrix' && hasPortraitPlatform) return <Suspense fallback={<p role="status">正在加载…</p>}><Matrix domain="shortvideo" isAdmin={isAdmin} loggedIn={loggedIn} onExit={() => setMode('browse')} /></Suspense>;
  return <section className="space-y-1.5">
    {/* 播放/平台操作与可展开的频道筛选分行，筛选面板使用内容区全宽。 */}
    <div className="flex flex-wrap items-center gap-1.5">
      {canMatrix ? <button className={chip(false)} onClick={() => setMode('matrix')}>多联放映</button> : null}
      {platforms.length > 1 ? <select aria-label="平台" className={select} value={platform} onChange={e => pickPlatform(e.target.value)}>
        {['', ...platforms].map(p => <option key={p || 'all'} value={p}>{p || '全部平台'}</option>)}
      </select> : null}
      <span className={`${divider} hidden sm:block`} />
      {channel === 'recommend' ? <button className={chip(false)} onClick={() => setSeed(newSeed())}>换一批</button> : null}
      {group && loggedIn ? <button className={chip(pins.includes(group))} onClick={() => setPinned(group, !pins.includes(group))} title={pins.includes(group) ? '取消固定这个博主' : '把这个博主固定在频道条前面'}>{pins.includes(group) ? '取消固定' : '📌 固定'}</button> : null}
      <span className={`text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 个`}</span>
    </div>
    <ChannelBar groups={shownGroups} pins={pins} channel={channel} onPick={setChannel} groupLabel="博主" loggedIn={loggedIn} onPin={loggedIn ? setPinned : undefined} />
    <VirtualGrid total={paged.total} minColumnWidth={isLandscapeGrid ? 240 : 170} aspect={isLandscapeGrid ? 9 / 16 : 16 / 9} extraHeight={44} render={index => {
      const post = paged.item(index);
      if (!post) return <div className="h-full rounded-lg border border-line bg-panel" />;
      const postLandscape = !!(post.width && post.height && post.width > post.height);
      const fitClass = !isLandscapeGrid && postLandscape ? 'object-contain' : 'object-cover';
      return <button className="flex h-full w-full flex-col overflow-hidden rounded-lg border border-line bg-panel text-left hover:border-primary" onClick={() => {
        if (getViewerFullscreenPref()) enterBrowserFullscreen();
        setOpen(index);
      }}>
        <div className="relative min-h-0 flex-1 bg-background">
          {post.has_thumbnail
            ? <img className={`h-full w-full ${fitClass}`} src={`/api/media/shortvideo/thumbnail?${query(post)}`} alt="" decoding="async" />
            : <span className="flex h-full items-center justify-center text-4xl text-line">{post.kind === 'album' ? '🖼' : '▶'}</span>}
          <span className="absolute right-1 top-1 rounded bg-black/60 px-1.5 text-xs text-white">{post.kind === 'album' ? `图集 ${post.image_count}` : '视频'}</span>
          {liked.isLiked(post) ? <span className="absolute left-1 top-1 text-[#f778ba]">♥</span> : null}
        </div>
        <div className="h-11 px-1.5 py-1"><p className="truncate text-xs">{post.title}</p><p className="truncate text-xs text-muted">{post.creator_id.split('/').slice(1).join(' / ')} · {post.published}</p></div>
      </button>;
    }} />
    {open !== undefined ? <Viewer post={paged.item(open)} liked={liked.isLiked(paged.item(open))} isAdmin={isAdmin} loggedIn={loggedIn} onLike={() => { const p = paged.item(open); if (p) liked.toggle(p); }}
      onTrash={() => { const p = paged.item(open); return p ? trash('shortvideo', p.id).then(() => setReload(n => n + 1)) : Promise.resolve(); }}
      onClose={() => { exitBrowserFullscreen(); setOpen(undefined); }}
      onPrev={open > 0 ? () => setOpen(open - 1) : undefined}
      onNext={open < paged.total - 1 ? () => setOpen(open + 1) : undefined} /> : null}
  </section>;
}

// Viewer：全屏播放视频或翻看图集。←/→ 切作品，↑/↓ 翻图，Z 点赞，Delete 按两次移到回收站，Esc 关闭。
// 行为上报：展示即 view；离开时 2 秒内划走算 skip，看完（或 80%）算 complete，停留 8 秒以上算 dwell。
function Viewer({ post, liked, isAdmin, loggedIn, onLike, onTrash, onClose, onPrev, onNext }: {
  post?: Post; liked: boolean; isAdmin: boolean; loggedIn: boolean; onLike(): void; onTrash(): Promise<void>; onClose(): void; onPrev?: () => void; onNext?: () => void;
}) {
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState('');
  const [armed, setArmed] = useState(false);
  const [paused, setPaused] = useState(false);
  const [controlsVisible, setControlsVisible] = useState(() => stored('omnihub.viewer.controls', true));
  const [fsPref, setFsPref] = useState(getViewerFullscreenPref);
  const [fillMode, setFillMode] = useState<'contain' | 'cover'>(() => stored('omnihub.sv.fill', 'contain'));
  const video = useRef<HTMLVideoElement>(null);
  const tapTimer = useRef(0);
  const lastCenterTap = useRef(0);
  useEffect(() => () => window.clearTimeout(tapTimer.current), []);
  useEffect(() => store('omnihub.viewer.controls', controlsVisible), [controlsVisible]);
  useEffect(() => store('omnihub.sv.fill', fillMode), [fillMode]);
  useEffect(() => {
    if (fsPref && !document.fullscreenElement) enterBrowserFullscreen();
  }, [fsPref, post?.id]);
  useEffect(() => {
    const onFs = () => setFsPref(Boolean(document.fullscreenElement) || getViewerFullscreenPref());
    document.addEventListener('fullscreenchange', onFs);
    return () => document.removeEventListener('fullscreenchange', onFs);
  }, []);
  const toggleFullscreen = () => {
    const next = !(Boolean(document.fullscreenElement) || fsPref);
    setViewerFullscreenPref(next);
    setFsPref(next);
    if (next) enterBrowserFullscreen();
    else exitBrowserFullscreen();
  };
  const togglePlayPause = () => {
    const v = video.current;
    if (!v) return;
    if (v.paused) void v.play();
    else v.pause();
  };
  // 静音和音量跨视频记住（存在浏览器里）：在一个视频上点了静音，后面的视频也静音
  const [sound, setSound] = useState(() => stored('omnihub.sv.sound', { muted: false, volume: 1 }));
  useEffect(() => store('omnihub.sv.sound', sound), [sound]);
  const applySound = (el: HTMLMediaElement | null) => { if (el) { el.muted = sound.muted; el.volume = sound.volume; } };
  const keepSound = (el: HTMLMediaElement) => setSound({ muted: el.muted, volume: el.volume });
  const toggleMute = () => {
    const nextMuted = !sound.muted;
    setSound(s => ({ ...s, muted: nextMuted }));
    if (video.current) video.current.muted = nextMuted;
  };
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
    setArmed(false);
    setPaused(false);
    finish();
    if (post) {
      shown.current = { post, at: Date.now(), pages: 1 };
      sendEvent('shortvideo', post.id, 'view');
      if (post.kind === 'video') video.current?.play().catch(() => undefined);
    }
  }, [post?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => finish(), []); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (shown.current) shown.current.pages = Math.max(shown.current.pages, page); }, [page]);
  useEffect(() => { if (!notice) return; const t = window.setTimeout(() => setNotice(''), 2500); return () => window.clearTimeout(t); }, [notice]);
  useEffect(() => { if (!armed) return; const t = window.setTimeout(() => setArmed(false), 3000); return () => window.clearTimeout(t); }, [armed]);
  const doTrash = () => {
    if (!post || !isAdmin) return;
    if (!armed) { setArmed(true); setNotice('再点一次移到回收站'); return; }
    setArmed(false); shown.current = undefined;
    video.current?.removeAttribute('src');
    onTrash().then(() => setNotice('已移到系统回收站')).catch((e: Error) => setNotice(e.message));
  };
  const keys = useRef({ onClose, onPrev, onNext, pages, isAdmin, loggedIn, onLike, doTrash, toggleFullscreen, togglePlayPause, toggleMute });
  keys.current = { onClose, onPrev, onNext, pages, isAdmin, loggedIn, onLike, doTrash, toggleFullscreen, togglePlayPause, toggleMute };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (mangaHandlesKeyboard()) return;
      const k = keys.current;
      if (e.key === 'Escape') k.onClose();
      else if (e.key === 'f' || e.key === 'F') k.toggleFullscreen();
      else if (e.key === 'm' || e.key === 'M') k.toggleMute();
      else if (e.key === ' ') k.togglePlayPause();
      else if (e.key === 'ArrowLeft') k.onPrev?.();
      else if (e.key === 'ArrowRight') k.onNext?.();
      else if (e.key === 'ArrowDown') setPage(p => Math.min(k.pages, p + 1));
      else if (e.key === 'ArrowUp') setPage(p => Math.max(1, p - 1));
      else if ((e.key === 'z' || e.key === 'Z') && k.loggedIn) k.onLike();
      else if (e.key === 'Delete' && k.isAdmin) k.doTrash();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  // 点画面：左边三分之一下一个、右边三分之一上一个（图集先翻图片，翻到头再换作品），中间单击唤起/关闭控件（视频双击中间暂停/继续）。
  // 滑动：左滑/上滑下一个、右滑上一个、下滑返回。控件展开时视频底部留出原生进度条。
  const press = useRef<{ x: number; y: number } | undefined>(undefined);
  const prevPage = () => { if (page > 1) setPage(page - 1); else onPrev?.(); };
  const nextPage = () => { if (page < pages) setPage(page + 1); else onNext?.(); };
  function release(e: ReactPointerEvent<HTMLDivElement>) {
    const start = press.current;
    press.current = undefined;
    if (!start || !post) return;
    const dx = e.clientX - start.x, dy = e.clientY - start.y;
    if (Math.abs(dx) > 55 && Math.abs(dx) > Math.abs(dy) * 1.2) { if (dx < 0) onNext?.(); else onPrev?.(); return; }
    if (dy > 90 && dy > Math.abs(dx) * 1.2) { onClose(); return; }
    if (dy < -80 && Math.abs(dy) > Math.abs(dx) * 1.2) { onNext?.(); return; }
    if (Math.abs(dx) > 12 || Math.abs(dy) > 12) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = (e.clientX - rect.left) / Math.max(1, rect.width);
    const isVideo = post.kind === 'video';
    if (x < 1 / 3) {
      if (isVideo) onNext?.(); else nextPage();
    } else if (x > 2 / 3) {
      if (isVideo) onPrev?.(); else prevPage();
    } else {
      const now = Date.now();
      if (isVideo && now - lastCenterTap.current < 260) {
        window.clearTimeout(tapTimer.current);
        lastCenterTap.current = 0;
        togglePlayPause();
      } else {
        lastCenterTap.current = now;
        window.clearTimeout(tapTimer.current);
        tapTimer.current = window.setTimeout(() => {
          setControlsVisible(v => !v);
        }, 220);
      }
    }
  }
  const fitClass = fillMode === 'cover' ? 'h-full w-full object-cover' : 'h-full w-full object-contain';
  const gestures = <div className={`absolute inset-x-0 top-0 z-10 touch-none ${controlsVisible && post?.kind === 'video' ? 'bottom-14' : 'bottom-0'}`}
    onPointerDown={e => { press.current = { x: e.clientX, y: e.clientY }; }} onPointerUp={release} onPointerCancel={() => { press.current = undefined; }} />;
  return <div role="dialog" aria-label="短视频查看器" className="fixed inset-y-0 left-0 z-50 overflow-hidden bg-black text-white select-none" style={{ right: 'var(--manga-dock, 0px)' }}>
    {controlsVisible ? <div className="absolute inset-x-0 top-0 z-20 flex flex-wrap items-center gap-2 bg-gradient-to-b from-black/85 via-black/50 to-transparent p-2.5 pb-6">
      <p className="min-w-0 flex-1 truncate text-sm">{post ? `${post.creator_id.split('/').slice(1).join(' / ')} · ${post.published} · ${post.title}` : '加载中…'}</p>
      {notice ? <span className={`text-sm ${armed ? 'font-bold text-[#ff7b72]' : 'text-[#3fb950]'}`}>{notice}</span> : null}
      {loggedIn ? <button className={iconButton} onClick={onLike} title="点赞（Z）">{liked ? <span className="text-[#f778ba]">♥</span> : '♡'}</button> : null}
      {isAdmin ? <button className={`${iconButton} ${armed ? 'bg-white/20' : ''}`} onClick={doTrash} title="移到系统回收站（Delete 按两次）">🗑</button> : null}
      {post?.kind === 'video' ? <>
        <button className={iconButton} onClick={togglePlayPause} title="暂停 / 播放（空格或双击画面中间）">{paused ? '▶' : '⏸'}</button>
        <button className={iconButton} onClick={toggleMute} title="静音切换（M）">{sound.muted ? '🔇' : '🔊'}</button>
      </> : null}
      <button className={button} onClick={() => setFillMode(m => m === 'contain' ? 'cover' : 'contain')} title="切换完整显示或裁切铺满屏幕">{fillMode === 'cover' ? '适应' : '铺满'}</button>
      <button className={`${button} ${fsPref ? 'border-primary text-primary' : ''}`} onClick={toggleFullscreen} title="全屏模式（F，跨媒体自动保持）">{fsPref ? '退出全屏' : '全屏'}</button>
      <button className={button} disabled={!onPrev} onClick={onPrev}>上一个</button>
      <button className={button} disabled={!onNext} onClick={onNext}>下一个</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div> : null}
    <div className="absolute inset-0 flex items-center justify-center overflow-hidden">
      {!post ? <p>加载中…</p> : post.kind === 'video' && !canPlayCodec(post.codec)
        ? <p className="max-w-md text-center">这个视频是 {post.codec?.toUpperCase()} 编码，当前浏览器无法解码。请在 Deck 上的 omniHub 客户端中播放。</p>
        : post.kind === 'video'
        ? <>{gestures}<video ref={el => { video.current = el; applySound(el); }} key={post.id} className={fitClass} src={`/api/media/shortvideo/stream?${query(post)}`} controls={controlsVisible} autoPlay playsInline
            onPlay={() => setPaused(false)}
            onPause={() => setPaused(true)}
            onVolumeChange={e => keepSound(e.currentTarget)}
            onEnded={() => { sendEvent('shortvideo', post.id, 'complete'); shown.current = undefined; onNext?.(); }} /></>
        : <>
          {gestures}
          <img key={post.id + page} className={fitClass} src={`/api/media/shortvideo/album/image?${query(post, { index: String(page) })}`} alt={`${page}/${pages}`} />
          {post.music ? <audio key={post.id} ref={applySound} onVolumeChange={e => keepSound(e.currentTarget)} src={`/api/media/shortvideo/stream?${query(post, { music: '1' })}`} autoPlay loop controls className={`absolute bottom-3 left-3 z-20 h-8 ${controlsVisible ? '' : 'hidden'}`} /> : null}
          {controlsVisible ? <div className="absolute bottom-3 right-3 z-20 flex items-center gap-2 rounded-md bg-black/65 px-2 py-1">
            <button className={button} disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一张</button>
            <span className="text-sm">{page} / {pages}</span>
            <button className={button} disabled={page >= pages} onClick={() => setPage(p => p + 1)}>下一张</button>
          </div> : null}
        </>}
    </div>
  </div>;
}
