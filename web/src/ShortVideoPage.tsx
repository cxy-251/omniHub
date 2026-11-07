import { lazy, Suspense, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { usePaged } from './paged';
import VirtualGrid from './VirtualGrid';
import ChannelBar from './ChannelBar';
import { mangaHandlesKeyboard } from './mangaState';
import { canPlayCodec, webCodecs } from './codecs';
import { feedPath, feedQuery, newSeed, sendEvent, trash, useGroups, useLiked } from './feed';
import {
  chip,
  divider,
  enterBrowserFullscreen,
  exitBrowserFullscreen,
  getViewerFullscreenPref,
  select,
  setViewerFullscreenPref,
  store,
  stored,
} from './ui';
import {
  IconClose,
  IconExitFullscreen,
  IconFill,
  IconFit,
  IconFullscreen,
  IconHeart,
  IconNext,
  IconPause,
  IconPlay,
  IconPrev,
  IconTrash,
  ViewerIconButton,
  VolumeSliderPill,
} from './ViewerControls';

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
    {open !== undefined ? <Viewer post={paged.item(open)}
      prevPost={open > 0 ? paged.item(open - 1) : undefined}
      nextPost={open < paged.total - 1 ? paged.item(open + 1) : undefined}
      liked={liked.isLiked(paged.item(open))} isAdmin={isAdmin} loggedIn={loggedIn} onLike={() => { const p = paged.item(open); if (p) liked.toggle(p); }}
      onTrash={() => { const p = paged.item(open); return p ? trash('shortvideo', p.id).then(() => setReload(n => n + 1)) : Promise.resolve(); }}
      onClose={() => { exitBrowserFullscreen(); setOpen(undefined); }}
      onPrev={open > 0 ? () => setOpen(open - 1) : undefined}
      onNext={open < paged.total - 1 ? () => setOpen(open + 1) : undefined} /> : null}
  </section>;
}

// Viewer：全屏播放视频或翻看图集。←/→ 切作品，↑/↓ 翻图，Z 点赞，Delete 按两次移到回收站，Esc 或鼠标右键关闭。
// 行为上报：展示即 view；离开时 2 秒内划走算 skip，看完（或 80%）算 complete，停留 8 秒以上算 dwell。
function Viewer({ post, prevPost, nextPost, liked, isAdmin, loggedIn, onLike, onTrash, onClose, onPrev, onNext }: {
  post?: Post; prevPost?: Post; nextPost?: Post; liked: boolean; isAdmin: boolean; loggedIn: boolean; onLike(): void; onTrash(): Promise<void>; onClose(): void; onPrev?: () => void; onNext?: () => void;
}) {
  const [page, setPage] = useState(1);
  const [notice, setNotice] = useState('');
  const [armed, setArmed] = useState(false);
  const [paused, setPaused] = useState(false);
  const [controlsVisible, setControlsVisible] = useState(() => stored('omnihub.viewer.controls', true));
  const [fsPref, setFsPref] = useState(getViewerFullscreenPref);
  const [fillMode, setFillMode] = useState<'contain' | 'cover'>(() => stored('omnihub.sv.fill', 'contain'));
  const video = useRef<HTMLVideoElement>(null);
  const audioEl = useRef<HTMLAudioElement | null>(null);
  const tapTimer = useRef(0);
  const slideTimer = useRef(0);
  const lastCenterTap = useRef(0);
  useEffect(() => () => { window.clearTimeout(tapTimer.current); window.clearTimeout(slideTimer.current); }, []);
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
  // 静音和音量跨视频记住（存在浏览器里）：在一个视频上调音量或静音，后面的视频也保持
  const [sound, setSound] = useState(() => stored('omnihub.sv.sound', { muted: false, volume: 1 }));
  useEffect(() => store('omnihub.sv.sound', sound), [sound]);
  const applySound = (el: HTMLMediaElement | null) => { if (el) { el.muted = sound.muted; el.volume = sound.volume; } };
  const keepSound = (el: HTMLMediaElement) => setSound({ muted: el.muted, volume: el.volume });
  const toggleMute = () => {
    const nextMuted = !sound.muted;
    const nextVol = !nextMuted && sound.volume === 0 ? 0.6 : sound.volume;
    setSound({ muted: nextMuted, volume: nextVol });
    if (video.current) { video.current.muted = nextMuted; video.current.volume = nextVol; }
    if (audioEl.current) { audioEl.current.muted = nextMuted; audioEl.current.volume = nextVol; }
  };
  const changeVolume = (vol: number) => {
    const v = Math.max(0, Math.min(1, vol));
    const nextMuted = v === 0;
    setSound({ muted: nextMuted, volume: v });
    if (video.current) { video.current.muted = nextMuted; video.current.volume = v; }
    if (audioEl.current) { audioEl.current.muted = nextMuted; audioEl.current.volume = v; }
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
  // 滑动：左右滑跟手过渡切换上/下一个作品，下拉跟手阻尼与退场动画关闭。控件展开时视频底部留出原生进度条。
  const press = useRef<{ x: number; y: number; mode: '' | 'x' | 'y' } | undefined>(undefined);
  const [dragX, setDragX] = useState(0);
  const [pullY, setPullY] = useState(0);
  const [closing, setClosing] = useState(false);
  const [animating, setAnimating] = useState(false);
  useEffect(() => {
    window.clearTimeout(slideTimer.current);
    setDragX(0);
    setPullY(0);
    setClosing(false);
    setAnimating(false);
  }, [post?.id, page]);
  const prevPage = () => { if (page > 1) setPage(page - 1); else onPrev?.(); };
  const nextPage = () => { if (page < pages) setPage(page + 1); else onNext?.(); };
  const triggerSlide = (dir: -1 | 1, action?: () => void) => {
    if (!action) { setAnimating(true); setDragX(0); return; }
    setAnimating(true);
    setDragX(dir * window.innerWidth);
    window.clearTimeout(slideTimer.current);
    slideTimer.current = window.setTimeout(() => {
      action();
    }, 170);
  };
  function move(e: ReactPointerEvent<HTMLDivElement>) {
    const start = press.current;
    if (!start || (!e.buttons && e.pointerType === 'mouse')) return;
    const dx = e.clientX - start.x, dy = e.clientY - start.y;
    if (!start.mode) {
      if (Math.abs(dx) > 10 && Math.abs(dx) >= Math.abs(dy)) start.mode = 'x';
      else if (dy > 12 && dy > Math.abs(dx)) start.mode = 'y';
    }
    if (start.mode === 'x') {
      const canGo = dx < 0 ? Boolean(onNext) : Boolean(onPrev);
      setDragX(canGo ? dx : dx * 0.25);
    } else if (start.mode === 'y') {
      setPullY(Math.max(0, dy * 0.85));
    }
  }
  function release(e: ReactPointerEvent<HTMLDivElement>) {
    const start = press.current;
    press.current = undefined;
    if (!start || !post) { setDragX(0); setPullY(0); return; }
    const dx = e.clientX - start.x, dy = e.clientY - start.y;
    if (start.mode === 'x' || (Math.abs(dx) > 55 && Math.abs(dx) > Math.abs(dy) * 1.15)) {
      setPullY(0);
      if (Math.abs(dx) > 55) {
        triggerSlide(dx < 0 ? -1 : 1, dx < 0 ? onNext : onPrev);
      } else {
        setAnimating(true);
        setDragX(0);
      }
      return;
    }
    if (dy > 90 && dy > Math.abs(dx) * 1.15) {
      setClosing(true);
      setPullY(window.innerHeight * 0.45);
      window.setTimeout(() => onClose(), 150);
      return;
    }
    setAnimating(true);
    setDragX(0);
    setPullY(0);
    if (dy < -80 && Math.abs(dy) > Math.abs(dx) * 1.2) { triggerSlide(-1, onNext); return; }
    if (Math.abs(dx) > 12 || Math.abs(dy) > 12) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = (e.clientX - rect.left) / Math.max(1, rect.width);
    const isVideo = post.kind === 'video';
    if (x < 1 / 3) {
      if (isVideo) triggerSlide(-1, onNext); else nextPage();
    } else if (x > 2 / 3) {
      if (isVideo) triggerSlide(1, onPrev); else prevPage();
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
  const pullScale = pullY > 0 ? Math.max(0.82, 1 - pullY / 950) : 1;
  const bgAlpha = Math.max(0.32, 1 - pullY / 380);
  const mediaMotionStyle: React.CSSProperties = {
    transform: `translate3d(${dragX}px, ${pullY}px, 0) scale(${pullScale})`,
    opacity: closing ? 0 : 1,
    transition: press.current && !animating ? 'none' : 'transform 170ms cubic-bezier(0.22, 1, 0.36, 1), opacity 150ms ease-out',
  };
  const gestures = <div className={`absolute inset-x-0 top-0 z-10 touch-none ${controlsVisible && post?.kind === 'video' ? 'bottom-14' : 'bottom-0'}`}
    onPointerDown={e => { if (e.pointerType === 'mouse' && e.button !== 0) return; setAnimating(false); press.current = { x: e.clientX, y: e.clientY, mode: '' }; }}
    onPointerMove={move} onPointerUp={release}
    onPointerCancel={() => { press.current = undefined; setAnimating(true); setDragX(0); setPullY(0); }} />;
  const renderPostPreview = (p?: Post) => {
    if (!p) return null;
    if (p.kind === 'album') {
      return <img className={fitClass} src={`/api/media/shortvideo/album/image?${query(p, { index: '1' })}`} alt="" draggable={false} />;
    }
    if (p.has_thumbnail) {
      return <img className={fitClass} src={`/api/media/shortvideo/thumbnail?${query(p)}`} alt="" draggable={false} />;
    }
    return <div className="flex h-full w-full items-center justify-center bg-black/80 text-4xl text-white/30">▶</div>;
  };
  return <div role="dialog" aria-label="短视频查看器"
    onContextMenu={e => { e.preventDefault(); e.stopPropagation(); onClose(); }}
    className="fixed inset-y-0 left-0 z-50 overflow-hidden text-white select-none transition-colors duration-75"
    style={{ right: 'var(--manga-dock, 0px)', backgroundColor: `rgba(0, 0, 0, ${bgAlpha})` }}>
    {controlsVisible ? <div className="absolute inset-x-0 top-0 z-20 flex flex-wrap items-center gap-2 bg-gradient-to-b from-black/85 via-black/45 to-transparent px-3 py-2.5 pb-6">
      <p className="min-w-0 flex-1 truncate text-sm font-medium drop-shadow">{post ? `${post.creator_id.split('/').slice(1).join(' / ')} · ${post.published} · ${post.title}` : '加载中…'}</p>
      {notice ? <span className={`text-sm ${armed ? 'font-bold text-[#ff7b72]' : 'text-[#3fb950]'}`}>{notice}</span> : null}
      {loggedIn ? <ViewerIconButton label={liked ? '取消点赞 (Z)' : '点赞 (Z)'} active={liked} onClick={onLike}><IconHeart filled={liked} /></ViewerIconButton> : null}
      {isAdmin ? <ViewerIconButton label={armed ? '确认移到系统回收站' : '移到系统回收站 (Delete×2)'} danger={armed} onClick={doTrash}><IconTrash /></ViewerIconButton> : null}
      {post?.kind === 'video' ? (
        <ViewerIconButton label={paused ? '继续播放 (空格 / 双击中间)' : '暂停播放 (空格 / 双击中间)'} onClick={togglePlayPause}>{paused ? <IconPlay /> : <IconPause />}</ViewerIconButton>
      ) : null}
      {post?.kind === 'video' || post?.music ? (
        <VolumeSliderPill volume={sound.volume} muted={sound.muted} onToggleMute={toggleMute} onChangeVolume={changeVolume} />
      ) : null}
      <ViewerIconButton label={fillMode === 'cover' ? '切换为完整适应' : '切换为裁切铺满'} active={fillMode === 'cover'} onClick={() => setFillMode(m => m === 'contain' ? 'cover' : 'contain')}>{fillMode === 'cover' ? <IconFit /> : <IconFill />}</ViewerIconButton>
      <ViewerIconButton label={fsPref ? '退出全屏 (F)' : '全屏模式 (F，跨媒体自动保持)'} active={fsPref} onClick={toggleFullscreen}>{fsPref ? <IconExitFullscreen /> : <IconFullscreen />}</ViewerIconButton>
      <ViewerIconButton label="上一个 (← / 右侧点击)" disabled={!onPrev} onClick={() => triggerSlide(1, onPrev)}><IconPrev /></ViewerIconButton>
      <ViewerIconButton label="下一个 (→ / 左侧点击)" disabled={!onNext} onClick={() => triggerSlide(-1, onNext)}><IconNext /></ViewerIconButton>
      <ViewerIconButton label="关闭 (Esc / 右键 / 下滑)" onClick={onClose}><IconClose /></ViewerIconButton>
    </div> : null}
    <div className="absolute inset-0 flex items-center justify-center overflow-hidden" style={mediaMotionStyle}>
      {prevPost ? <div className="pointer-events-none absolute inset-y-0 -left-full flex h-full w-full items-center justify-center pr-3">{renderPostPreview(prevPost)}</div> : null}
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
          {post.music ? <audio key={post.id} ref={el => { audioEl.current = el; applySound(el); }} onVolumeChange={e => keepSound(e.currentTarget)} src={`/api/media/shortvideo/stream?${query(post, { music: '1' })}`} autoPlay loop controls className={`absolute bottom-3 left-3 z-20 h-8 ${controlsVisible ? '' : 'hidden'}`} /> : null}
          {controlsVisible ? <div className="absolute bottom-3 right-3 z-20 flex items-center gap-2 rounded-full border border-white/15 bg-black/65 px-2.5 py-1 backdrop-blur-md">
            <ViewerIconButton label="上一张图片 (↑)" disabled={page <= 1} onClick={() => setPage(p => p - 1)}><IconPrev /></ViewerIconButton>
            <span className="px-1 text-xs font-medium">{page} / {pages}</span>
            <ViewerIconButton label="下一张图片 (↓)" disabled={page >= pages} onClick={() => setPage(p => p + 1)}><IconNext /></ViewerIconButton>
          </div> : null}
        </>}
      {nextPost ? <div className="pointer-events-none absolute inset-y-0 left-full flex h-full w-full items-center justify-center pl-3">{renderPostPreview(nextPost)}</div> : null}
    </div>
  </div>;
}
