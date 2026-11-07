import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import MasonryGrid from './MasonryGrid';
import ChannelBar from './ChannelBar';
import { mangaHandlesKeyboard } from './mangaState';
import { feedPath, feedQuery, newSeed, restore, sendEvent, trash, useGroups, useLiked } from './feed';
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

type Photo = { id: string; filename: string; width: number; height: number; color_hex: string; library_id: string; collection?: string; liked?: boolean };
// 图库（空是原来的相册）也要带上，缩略图和原图在各自图库里
const query = (p: Photo) => new URLSearchParams({ library_id: p.library_id, id: p.id, ...(p.collection ? { collection: p.collection } : {}) }).toString();

// 相册（只读）：先选平台（全部 | 相册 | TWICE | …），再选频道（推荐 | 我的点赞 | 色系 | 回收站）的瀑布流 + 原图查看 + 多联放映（图片自动轮播）。
// 回收站只在 omniHub 里隐藏，不改原图库。与桌面端共用 /api/feed/gallery。
export default function GalleryPage({ active, isAdmin, loggedIn = false }: { active: boolean; isAdmin: boolean; loggedIn?: boolean }) {
  const [mode, setMode] = useState<'browse' | 'matrix'>(() => window.location.hash.endsWith('/matrix') ? 'matrix' : stored('omnihub.gallery.mode', 'browse'));
  const legacy = stored('omnihub.gallery.channel', 'recommend');
  const [channel, setChannel] = useState(() => (legacy.startsWith('group:coll:') || legacy.startsWith('group:nsfw:')) ? 'recommend' : legacy);
  const [platform, setPlatform] = useState(() => stored('omnihub.gallery.platform', ''));
  const [seed, setSeed] = useState(newSeed);
  const [reload, setReload] = useState(0);
  const [open, setOpen] = useState<number>();
  const [ratios, setRatios] = useState<number[]>([]);
  useEffect(() => store('omnihub.gallery.mode', mode), [mode]);
  useEffect(() => store('omnihub.gallery.channel', channel), [channel]);
  useEffect(() => store('omnihub.gallery.platform', platform), [platform]);
  const { groups, pins } = useGroups('gallery', active);
  const liked = useLiked('gallery');
  const platforms = [...new Set(groups.map(g => g.platform).filter(Boolean))] as string[];
  const shownGroups = groups.filter(g => (g.platform || '') === platform && g.count > 0);
  useEffect(() => { if (platform && groups.length && !platforms.includes(platform)) setPlatform(''); }, [platform, groups]); // eslint-disable-line react-hooks/exhaustive-deps
  const pickPlatform = (p: string) => { setPlatform(p); if (channel.startsWith('group:') && !groups.some(g => `group:${g.id}` === channel && (g.platform || '') === p && g.count > 0)) setChannel('recommend'); };
  const browsing = active && mode === 'browse';
  const q = feedQuery({ channel, scope: platform ? `tag:platform:${platform}` : undefined, seed, r: reload || undefined });
  const paged = usePaged<Photo>(browsing ? `${feedPath('gallery', 'items')}?${q}` : '', 100);
  useEffect(() => {
    if (!browsing) return;
    const controller = new AbortController();
    api<{ ratios: number[] }>(`${feedPath('gallery', 'ratios')}?${q}`, { signal: controller.signal }).then(r => setRatios(r.ratios)).catch(() => undefined);
    return () => controller.abort();
  }, [browsing, q]);
  useEffect(() => { if (open !== undefined && paged.total > 0 && open >= paged.total) setOpen(paged.total - 1); }, [open, paged.total]);

  if (!active) return null;
  if (mode === 'matrix') return <Suspense fallback={<p role="status">正在加载…</p>}><Matrix domain="gallery" isAdmin={isAdmin} loggedIn={loggedIn} onExit={() => setMode('browse')} /></Suspense>;
  return <section className="space-y-1.5">
    {/* 可展开的分类筛选独占一行，避免挤成横向滚动条。 */}
    <div className="flex flex-wrap items-center gap-1.5">
      <button className={chip(false)} onClick={() => setMode('matrix')}>多联放映</button>
      {platforms.length > 0 ? <select aria-label="平台" className={select} value={platform} onChange={e => pickPlatform(e.target.value)}>
        {['', ...platforms].map(p => <option key={p || 'all'} value={p}>{p || '全部平台'}</option>)}
      </select> : null}
      <span className={divider} />
      {channel === 'recommend' ? <button className={chip(false)} onClick={() => setSeed(newSeed())}>换一批</button> : null}
      <span className={`text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 张${channel === 'hidden' ? '（回收站）' : ''}`}</span>
    </div>
    <ChannelBar groups={shownGroups} pins={pins} channel={channel} onPick={setChannel} showHidden={isAdmin} loggedIn={loggedIn} />
    <MasonryGrid ratios={ratios} minColumnWidth={180} render={i => {
      const photo = paged.item(i);
      return <button className="relative block h-full w-full overflow-hidden rounded" style={{ background: photo?.color_hex || 'var(--surface)' }} onClick={() => {
        if (!photo) return;
        if (getViewerFullscreenPref()) enterBrowserFullscreen();
        setOpen(i);
      }}>
        {photo ? <img className="h-full w-full object-cover" src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" decoding="async" /> : null}
        {liked.isLiked(photo) ? <span className="absolute left-1 top-1 text-[#f778ba]">♥</span> : null}
      </button>;
    }} />
    {open !== undefined ? <Viewer index={open} total={paged.total} photo={paged.item(open)} liked={liked.isLiked(paged.item(open))} restoring={channel === 'hidden'}
      isAdmin={isAdmin} loggedIn={loggedIn}
      onLike={() => { const p = paged.item(open); if (p) liked.toggle(p); }}
      onTrash={() => { const p = paged.item(open); if (!p) return Promise.resolve(); return (channel === 'hidden' ? restore('gallery', p.id) : trash('gallery', p.id)).then(() => setReload(n => n + 1)); }}
      onIndex={setOpen} onClose={() => { exitBrowserFullscreen(); setOpen(undefined); }} /> : null}
  </section>;
}

// Viewer：原图查看。先显示缩略图，原图到达后替换；←/→ 翻页，Z 点赞，Delete 按两次移到回收站，Esc 关闭（放大时先复位）。
// 滚轮/双指缩放、双击放大、拖动平移，+/-/0 键缩放与复位；⤓ 下载原图（原文件名）。
// 行为上报：停留 0.6 秒以上记为看过；离开时 1 秒内划走算 skip，停留 4 秒以上算 dwell。
function Viewer({ index, total, photo, liked, restoring, isAdmin, loggedIn, onLike, onTrash, onIndex, onClose }: {
  index: number; total: number; photo?: Photo; liked: boolean; restoring: boolean; isAdmin: boolean; loggedIn: boolean; onLike(): void; onTrash(): Promise<void>; onIndex(i: number): void; onClose(): void;
}) {
  const [loaded, setLoaded] = useState('');
  const [notice, setNotice] = useState('');
  const [armed, setArmed] = useState(false);
  const [controlsVisible, setControlsVisible] = useState(() => stored('omnihub.viewer.controls', true));
  const [fsPref, setFsPref] = useState(getViewerFullscreenPref);
  const [fillMode, setFillMode] = useState<'contain' | 'cover'>(() => stored('omnihub.gallery.fill', 'contain'));
  useEffect(() => store('omnihub.viewer.controls', controlsVisible), [controlsVisible]);
  useEffect(() => store('omnihub.gallery.fill', fillMode), [fillMode]);
  useEffect(() => {
    if (fsPref && !document.fullscreenElement) enterBrowserFullscreen();
  }, [fsPref, index]);
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
  const shown = useRef<{ id: string; at: number } | undefined>(undefined);
  const finish = () => {
    const s = shown.current;
    if (!s) return;
    const ms = Date.now() - s.at;
    if (ms < 1000) sendEvent('gallery', s.id, 'skip'); else if (ms >= 4000) sendEvent('gallery', s.id, 'dwell');
    shown.current = undefined;
  };
  useEffect(() => {
    finish();
    if (!photo) return;
    shown.current = { id: photo.id, at: Date.now() };
    const t = window.setTimeout(() => sendEvent('gallery', photo.id, 'view'), 600);
    return () => window.clearTimeout(t);
  }, [photo?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => finish(), []); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (!notice) return; const t = window.setTimeout(() => setNotice(''), 2500); return () => window.clearTimeout(t); }, [notice]);
  useEffect(() => { if (!armed) return; const t = window.setTimeout(() => setArmed(false), 3000); return () => window.clearTimeout(t); }, [armed]);
  const doTrash = () => {
    if (!photo || !isAdmin) return;
    if (!armed) { setArmed(true); setNotice(restoring ? '再点一次从回收站恢复' : '再点一次移到回收站'); return; }
    setArmed(false); shown.current = undefined;
    onTrash().then(() => setNotice(restoring ? '已恢复' : '已移到回收站，可在「🗑 回收站」里恢复')).catch((e: Error) => setNotice(e.message));
  };
  const goPrev = () => { if (index > 0) onIndex(index - 1); };
  const goNext = () => { if (index < total - 1) onIndex(index + 1); };
  const zoom = useZoom(photo?.id, { onSwipeLeft: goNext, onSwipeRight: goPrev, onSwipeDown: onClose });
  const tapTimer = useRef(0);
  useEffect(() => () => window.clearTimeout(tapTimer.current), []);
  const keys = useRef({ index, total, isAdmin, loggedIn, onIndex, onClose, onLike, doTrash, zoom, toggleFullscreen });
  keys.current = { index, total, isAdmin, loggedIn, onIndex, onClose, onLike, doTrash, zoom, toggleFullscreen };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (mangaHandlesKeyboard()) return;
      const k = keys.current;
      if (e.key === 'Escape' && k.zoom.zoomed) k.zoom.reset();
      else if (e.key === 'Escape') k.onClose();
      else if (e.key === 'f' || e.key === 'F') k.toggleFullscreen();
      else if (e.key === ' ') setControlsVisible(v => !v);
      else if (e.key === '+' || e.key === '=') k.zoom.step(1.5);
      else if (e.key === '-') k.zoom.step(1 / 1.5);
      else if (e.key === '0') k.zoom.reset();
      else if (e.key === 'ArrowRight' && k.index < k.total - 1) k.onIndex(k.index + 1);
      else if (e.key === 'ArrowLeft' && k.index > 0) k.onIndex(k.index - 1);
      else if ((e.key === 'z' || e.key === 'Z') && k.loggedIn) k.onLike();
      else if (e.key === 'Delete' && k.isAdmin) k.doTrash();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  const fitClass = fillMode === 'cover' ? 'h-full w-full object-cover' : 'h-full w-full object-contain';
  return <div role="dialog" aria-label="图片查看器" className="fixed inset-y-0 left-0 z-50 overflow-hidden bg-black text-white select-none" style={{ right: 'var(--manga-dock, 0px)' }}>
    {controlsVisible ? <div className="absolute inset-x-0 top-0 z-20 flex flex-wrap items-center gap-2 bg-gradient-to-b from-black/85 via-black/50 to-transparent p-2.5 pb-6">
      <p className="min-w-0 flex-1 truncate text-sm">{index + 1} / {total}{photo ? ` · ${photo.width}×${photo.height}` : ''}</p>
      {notice ? <span className={`text-sm ${armed ? 'font-bold text-[#ff7b72]' : 'text-[#3fb950]'}`}>{notice}</span> : null}
      {loggedIn ? <button className={iconButton} onClick={onLike} title="点赞（Z）">{liked ? <span className="text-[#f778ba]">♥</span> : '♡'}</button> : null}
      {isAdmin ? <button className={`${iconButton} ${armed ? 'bg-white/20' : ''}`} onClick={doTrash} title={restoring ? '从回收站恢复（Delete 按两次）' : '移到回收站（Delete 按两次；只在 omniHub 里隐藏）'}>{restoring ? '↩' : '🗑'}</button> : null}
      {photo ? <a className={iconButton} href={`/api/media/gallery/original?${query(photo)}&download=1`} download={photo.filename} title="下载原图">⤓</a> : null}
      <button className={button} onClick={() => setFillMode(m => m === 'contain' ? 'cover' : 'contain')} title="切换完整显示或裁切铺满屏幕">{fillMode === 'cover' ? '适应' : '铺满'}</button>
      <button className={`${button} ${fsPref ? 'border-primary text-primary' : ''}`} onClick={toggleFullscreen} title="全屏模式（F，跨媒体自动保持）">{fsPref ? '退出全屏' : '全屏'}</button>
      <button className={button} disabled={index <= 0} onClick={goPrev}>上一张</button>
      <button className={button} disabled={index >= total - 1} onClick={goNext}>下一张</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div> : null}
    <div className="absolute inset-0 flex touch-none items-center justify-center overflow-hidden" {...zoom.handlers}
      onClick={e => {
        // 单击等 220ms，期间第二次点击（双击缩放）会取消它。
        // 左 1/3：下一张；右 1/3：上一张；中间 1/3：唤起或隐藏控件。
        window.clearTimeout(tapTimer.current);
        if (zoom.consumeClick() || e.detail > 1) return;
        const r = e.currentTarget.getBoundingClientRect();
        const ratio = (e.clientX - r.left) / Math.max(1, r.width);
        tapTimer.current = window.setTimeout(() => {
          if (ratio < 1 / 3) goNext();
          else if (ratio > 2 / 3) goPrev();
          else setControlsVisible(v => !v);
        }, 220);
      }}>
      {photo ? <div className="flex h-full w-full items-center justify-center" style={zoom.style}>
        {loaded !== photo.id ? <img className={`absolute ${fitClass}`} src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" draggable={false} /> : null}
        <img key={photo.id} className={`relative ${fitClass}`} src={`/api/media/gallery/original?${query(photo)}`} alt={photo.filename} draggable={false} onLoad={() => setLoaded(photo.id)} />
      </div> : <p>加载中…</p>}
    </div>
  </div>;
}

// useZoom：大图缩放与平移。滚轮或双指捏合 1～5 倍，双击在 1 倍与 2.5 倍间切换，放大后拖动平移；
// 未放大时支持左右滑动切换图片、下滑关闭；放大状态或刚拖动过时，点击不触发翻页。key 变化（换图）时复位。
function useZoom(
  key: string | undefined,
  swipe?: { onSwipeLeft?: () => void; onSwipeRight?: () => void; onSwipeDown?: () => void },
) {
  const [view, setView] = useState({ scale: 1, x: 0, y: 0 });
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef({ moved: false, pinch: 0, startX: 0, startY: 0 });
  useEffect(() => setView({ scale: 1, x: 0, y: 0 }), [key]);
  const clamp = (scale: number) => Math.min(5, Math.max(1, scale));
  // 以 (cx, cy)（相对容器中心）为不动点缩放；to 由当前倍数算出新倍数（连续事件不会读到旧状态）。
  const zoomAt = (to: (scale: number) => number, cx: number, cy: number) => setView(v => {
    const next = clamp(to(v.scale));
    if (next === 1) return { scale: 1, x: 0, y: 0 };
    const k = next / v.scale;
    return { scale: next, x: cx - (cx - v.x) * k, y: cy - (cy - v.y) * k };
  });
  const center = (e: { clientX: number; clientY: number; currentTarget: Element }) => {
    const r = e.currentTarget.getBoundingClientRect();
    return [e.clientX - r.left - r.width / 2, e.clientY - r.top - r.height / 2];
  };
  const handlers = {
    onWheel: (e: React.WheelEvent) => { const [cx, cy] = center(e); zoomAt(s => s * (e.deltaY < 0 ? 1.2 : 1 / 1.2), cx, cy); },
    onDoubleClick: (e: React.MouseEvent) => { const [cx, cy] = center(e); zoomAt(s => s > 1 ? 1 : 2.5, cx, cy); },
    onPointerDown: (e: React.PointerEvent) => {
      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pointers.current.size === 1) gesture.current = { moved: false, pinch: 0, startX: e.clientX, startY: e.clientY };
      if (pointers.current.size === 2) { const [a, b] = [...pointers.current.values()]; gesture.current.pinch = Math.hypot(a.x - b.x, a.y - b.y); }
    },
    onPointerMove: (e: React.PointerEvent) => {
      const last = pointers.current.get(e.pointerId);
      if (!last) return;
      const dx = e.clientX - last.x, dy = e.clientY - last.y;
      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pointers.current.size === 2) {
        const [a, b] = [...pointers.current.values()];
        const dist = Math.hypot(a.x - b.x, a.y - b.y);
        const ratio = gesture.current.pinch > 0 ? dist / gesture.current.pinch : 1;
        if (gesture.current.pinch > 0) {
          const r = e.currentTarget.getBoundingClientRect();
          zoomAt(s => s * ratio, (a.x + b.x) / 2 - r.left - r.width / 2, (a.y + b.y) / 2 - r.top - r.height / 2);
        }
        gesture.current = { ...gesture.current, moved: true, pinch: dist };
      } else if (view.scale > 1 && (e.buttons || e.pointerType !== 'mouse')) {
        if (Math.abs(dx) + Math.abs(dy) > 0) gesture.current.moved = true;
        setView(v => ({ ...v, x: v.x + dx, y: v.y + dy }));
      }
    },
    onPointerUp: (e: React.PointerEvent) => {
      const wasSingle = pointers.current.size === 1;
      pointers.current.delete(e.pointerId);
      if (wasSingle && view.scale === 1 && !gesture.current.moved) {
        const totalDx = e.clientX - gesture.current.startX;
        const totalDy = e.clientY - gesture.current.startY;
        if (Math.abs(totalDx) > 55 && Math.abs(totalDx) > Math.abs(totalDy) * 1.2) {
          gesture.current.moved = true;
          if (totalDx < 0) swipe?.onSwipeLeft?.();
          else swipe?.onSwipeRight?.();
        } else if (totalDy > 90 && totalDy > Math.abs(totalDx) * 1.2) {
          gesture.current.moved = true;
          swipe?.onSwipeDown?.();
        }
      }
    },
    onPointerCancel: (e: React.PointerEvent) => { pointers.current.delete(e.pointerId); },
  };
  return {
    handlers,
    zoomed: view.scale > 1,
    reset: () => setView({ scale: 1, x: 0, y: 0 }),
    step: (factor: number) => zoomAt(s => s * factor, 0, 0),
    // 放大状态或刚拖动/滑动过时，点击不翻页。
    consumeClick: () => { const used = view.scale > 1 || gesture.current.moved; gesture.current.moved = false; return used; },
    style: { transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`, transformOrigin: 'center', transition: pointers.current.size ? 'none' : 'transform 80ms ease-out' } as React.CSSProperties,
  };
}
