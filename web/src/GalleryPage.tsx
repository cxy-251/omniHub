import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import MasonryGrid from './MasonryGrid';
import ChannelBar from './ChannelBar';
import { feedPath, feedQuery, newSeed, restore, sendEvent, trash, useGroups, useLiked } from './feed';
import { button, chip, iconButton, store, stored } from './ui';

const Matrix = lazy(() => import('./Matrix'));

type Photo = { id: string; filename: string; width: number; height: number; color_hex: string; library_id: string; liked?: boolean };
const query = (p: Photo) => new URLSearchParams({ library_id: p.library_id, id: p.id }).toString();

// 相册（只读）：频道（推荐 | 我的点赞 | 色系 | 回收站）的瀑布流 + 原图查看 + 多联放映（图片自动轮播）。
// 回收站只在 omniHub 里隐藏，不改原图库。与桌面端共用 /api/feed/gallery。
export default function GalleryPage({ active, isAdmin }: { active: boolean; isAdmin: boolean }) {
  const [mode, setMode] = useState<'browse' | 'matrix'>(() => window.location.hash.endsWith('/matrix') ? 'matrix' : stored('omnihub.gallery.mode', 'browse'));
  const [channel, setChannel] = useState(() => stored('omnihub.gallery.channel', 'recommend'));
  const [seed, setSeed] = useState(newSeed);
  const [reload, setReload] = useState(0);
  const [open, setOpen] = useState<number>();
  const [ratios, setRatios] = useState<number[]>([]);
  useEffect(() => store('omnihub.gallery.mode', mode), [mode]);
  useEffect(() => store('omnihub.gallery.channel', channel), [channel]);
  const { groups, pins } = useGroups('gallery', active);
  const liked = useLiked('gallery');
  const browsing = active && mode === 'browse';
  const q = feedQuery({ channel, seed, r: reload || undefined });
  const paged = usePaged<Photo>(browsing ? `${feedPath('gallery', 'items')}?${q}` : '', 100);
  useEffect(() => {
    if (!browsing) return;
    const controller = new AbortController();
    api<{ ratios: number[] }>(`${feedPath('gallery', 'ratios')}?${q}`, { signal: controller.signal }).then(r => setRatios(r.ratios)).catch(() => undefined);
    return () => controller.abort();
  }, [browsing, q]);
  useEffect(() => { if (open !== undefined && paged.total > 0 && open >= paged.total) setOpen(paged.total - 1); }, [open, paged.total]);

  if (!active) return null;
  if (mode === 'matrix') return <Suspense fallback={<p role="status">正在加载…</p>}><Matrix domain="gallery" onExit={() => setMode('browse')} /></Suspense>;
  return <section className="space-y-1.5">
    <div className="flex flex-wrap items-center gap-1.5">
      <button className={chip(true)}>浏览</button>
      <button className={chip(false)} onClick={() => setMode('matrix')}>多联放映</button>
      <span className="ml-auto" />
      {channel === 'recommend' ? <button className={chip(false)} onClick={() => setSeed(newSeed())}>换一批</button> : null}
      <span className={`text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 张${channel === 'hidden' ? '（回收站，可恢复）' : ''}`}</span>
    </div>
    <ChannelBar groups={groups} pins={pins} channel={channel} onPick={setChannel} showHidden={isAdmin} />
    <MasonryGrid ratios={ratios} minColumnWidth={180} render={i => {
      const photo = paged.item(i);
      return <button className="relative block h-full w-full overflow-hidden rounded" style={{ background: photo?.color_hex || 'var(--surface)' }} onClick={() => photo && setOpen(i)}>
        {photo ? <img className="h-full w-full object-cover" src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" decoding="async" /> : null}
        {liked.isLiked(photo) ? <span className="absolute left-1 top-1 text-[#f778ba]">♥</span> : null}
      </button>;
    }} />
    {open !== undefined ? <Viewer index={open} total={paged.total} photo={paged.item(open)} liked={liked.isLiked(paged.item(open))} restoring={channel === 'hidden'}
      onLike={() => { const p = paged.item(open); if (p) liked.toggle(p); }}
      onTrash={() => { const p = paged.item(open); if (!p) return Promise.resolve(); return (channel === 'hidden' ? restore('gallery', p.id) : trash('gallery', p.id)).then(() => setReload(n => n + 1)); }}
      onIndex={setOpen} onClose={() => setOpen(undefined)} /> : null}
  </section>;
}

// Viewer：原图查看。先显示缩略图，原图到达后替换；←/→ 翻页，Z 点赞，Delete 按两次移到回收站，Esc 关闭。
// 行为上报：停留 0.6 秒以上记为看过；离开时 1 秒内划走算 skip，停留 4 秒以上算 dwell。
function Viewer({ index, total, photo, liked, restoring, onLike, onTrash, onIndex, onClose }: {
  index: number; total: number; photo?: Photo; liked: boolean; restoring: boolean; onLike(): void; onTrash(): Promise<void>; onIndex(i: number): void; onClose(): void;
}) {
  const [loaded, setLoaded] = useState('');
  const [notice, setNotice] = useState('');
  const [armed, setArmed] = useState(false);
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
    if (!photo) return;
    if (!armed) { setArmed(true); setNotice(restoring ? '再点一次从回收站恢复' : '再点一次移到回收站'); return; }
    setArmed(false); shown.current = undefined;
    onTrash().then(() => setNotice(restoring ? '已恢复' : '已移到回收站，可在「🗑 回收站」里恢复')).catch((e: Error) => setNotice(e.message));
  };
  const keys = useRef({ index, total, onIndex, onClose, onLike, doTrash });
  keys.current = { index, total, onIndex, onClose, onLike, doTrash };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const k = keys.current;
      if (e.key === 'Escape') k.onClose();
      else if (e.key === 'ArrowRight' && k.index < k.total - 1) k.onIndex(k.index + 1);
      else if (e.key === 'ArrowLeft' && k.index > 0) k.onIndex(k.index - 1);
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
      <p className="min-w-0 flex-1 truncate text-sm">{index + 1} / {total}{photo ? ` · ${photo.width}×${photo.height}` : ''}</p>
      {notice ? <span className={`text-sm ${armed ? 'font-bold text-[#ff7b72]' : 'text-[#3fb950]'}`}>{notice}</span> : null}
      <button className={iconButton} onClick={onLike} title="点赞（Z）">{liked ? <span className="text-[#f778ba]">♥</span> : '♡'}</button>
      <button className={`${iconButton} ${armed ? 'bg-white/20' : ''}`} onClick={doTrash} title={restoring ? '从回收站恢复（Delete 按两次）' : '移到回收站（Delete 按两次；只在 omniHub 里隐藏）'}>{restoring ? '↩' : '🗑'}</button>
      <button className={button} disabled={index <= 0} onClick={() => onIndex(index - 1)}>上一张</button>
      <button className={button} disabled={index >= total - 1} onClick={() => onIndex(index + 1)}>下一张</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div>
    <div className="relative flex min-h-0 flex-1 items-center justify-center" onClick={e => { const r = e.currentTarget.getBoundingClientRect(); onIndex(Math.max(0, Math.min(total - 1, index + (e.clientX - r.left < r.width / 3 ? -1 : 1)))); }}>
      {photo ? <>
        {loaded !== photo.id ? <img className="absolute max-h-full max-w-full object-contain" src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" /> : null}
        <img key={photo.id} className="relative max-h-full max-w-full object-contain" src={`/api/media/gallery/original?${query(photo)}`} alt={photo.filename} onLoad={() => setLoaded(photo.id)} />
      </> : <p>加载中…</p>}
    </div>
  </div>;
}
