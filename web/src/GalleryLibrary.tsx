import { useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import MasonryGrid from './MasonryGrid';

type Photo = { id: string; filename: string; width: number; height: number; color_hex: string; library_id: string };

// color_family 0~9（定义见 gallery 项目 color_service.ClassifyColor）。
const colors = [['红', '#ef4444'], ['橙', '#f97316'], ['黄', '#eab308'], ['绿', '#22c55e'], ['蓝', '#3b82f6'], ['紫', '#a855f7'], ['粉', '#ec4899'], ['白', '#f1f5f9'], ['黑', '#0f172a'], ['灰', '#78716c']] as const;
const chip = (on: boolean) => `shrink-0 rounded-full border px-2.5 py-0.5 text-sm ${on ? 'border-primary text-primary' : 'border-line text-muted hover:text-foreground'}`;
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';
const query = (p: Photo) => new URLSearchParams({ library_id: p.library_id, id: p.id }).toString();

// GalleryLibrary：只读相册。网格虚拟滚动、按页懒加载；隐藏时卸载网格与查看器，释放图片。
export default function GalleryLibrary({ active }: { active: boolean; canEdit?: boolean }) {
  const [color, setColor] = useState(-1);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [open, setOpen] = useState<number>();
  const [ratios, setRatios] = useState<number[]>([]);
  const paged = usePaged<Photo>(active ? `/api/media/gallery/list${color >= 0 ? `?color=${color}` : ''}` : '', 100);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Record<string, number>>('/api/media/gallery/colors', { signal: controller.signal }).then(setCounts).catch(() => undefined);
    return () => controller.abort();
  }, [active]);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<{ ratios: number[] }>(`/api/media/gallery/ratios${color >= 0 ? `?color=${color}` : ''}`, { signal: controller.signal })
      .then(r => setRatios(r.ratios)).catch(() => undefined);
    return () => controller.abort();
  }, [active, color]);

  if (!active) return null;
  return <section className="space-y-2">
    <div className="flex items-center gap-1.5 overflow-x-auto pb-1">
      <button className={chip(color < 0)} onClick={() => setColor(-1)}>全部</button>
      {colors.map(([name, swatch], i) => <button key={i} className={chip(color === i)} onClick={() => setColor(i)}>
        <span className="mr-1 inline-block h-2.5 w-2.5 rounded-full border border-line align-middle" style={{ background: swatch }} />{name} · {counts[i] ?? 0}
      </button>)}
      <span className={`ml-auto shrink-0 text-xs ${paged.error ? 'text-danger' : 'text-muted'}`}>{paged.error || `${paged.total} 张 · 只读`}</span>
    </div>
    <MasonryGrid ratios={ratios} minColumnWidth={180} render={i => {
      const photo = paged.item(i);
      return <button className="block h-full w-full overflow-hidden rounded" style={{ background: photo?.color_hex || 'var(--surface)' }} onClick={() => photo && setOpen(i)}>
        {photo ? <img className="h-full w-full object-cover" src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" decoding="async" /> : null}
      </button>;
    }} />
    {open !== undefined ? <Viewer index={open} total={paged.total} photo={paged.item(open)} onIndex={setOpen} onClose={() => setOpen(undefined)} /> : null}
  </section>;
}

// Viewer：原图查看。先显示缩略图，原图到达后替换；←/→ 翻页，Esc 关闭。
function Viewer({ index, total, photo, onIndex, onClose }: { index: number; total: number; photo?: Photo; onIndex(i: number): void; onClose(): void }) {
  const [loaded, setLoaded] = useState('');
  const keys = useRef({ index, total, onIndex, onClose });
  keys.current = { index, total, onIndex, onClose };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const k = keys.current;
      if (e.key === 'Escape') k.onClose();
      else if (e.key === 'ArrowRight' && k.index < k.total - 1) k.onIndex(k.index + 1);
      else if (e.key === 'ArrowLeft' && k.index > 0) k.onIndex(k.index - 1);
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);
  return <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex flex-col bg-black/95 text-white">
    <div className="flex items-center gap-3 p-3">
      <p className="min-w-0 flex-1 truncate text-sm">{index + 1} / {total}{photo ? ` · ${photo.filename} · ${photo.width}×${photo.height}` : ''}</p>
      <button className={button} disabled={index <= 0} onClick={() => onIndex(index - 1)}>上一张</button>
      <button className={button} disabled={index >= total - 1} onClick={() => onIndex(index + 1)}>下一张</button>
      <button className={button} onClick={onClose}>关闭</button>
    </div>
    <div className="relative flex min-h-0 flex-1 items-center justify-center">
      {photo ? <>
        {loaded !== photo.id ? <img className="absolute max-h-full max-w-full object-contain" src={`/api/media/gallery/thumbnail?${query(photo)}`} alt="" /> : null}
        <img key={photo.id} className="relative max-h-full max-w-full object-contain" src={`/api/media/gallery/original?${query(photo)}`} alt={photo.filename} onLoad={() => setLoaded(photo.id)} />
      </> : <p>加载中…</p>}
    </div>
  </div>;
}
