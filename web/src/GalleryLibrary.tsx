import { useEffect, useState } from 'react';
import { request } from './api';

type Photo = { id: string; filename: string; size_bytes: number; width: number; height: number; library_id: string; library_name: string; read_only: boolean; deleted_at?: string };
type Props = { active: boolean; canEdit: boolean };
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

function photoURL(kind: string, photo: Photo) {
  return `/api/media/gallery/${kind}?${new URLSearchParams({ id: photo.id, library_id: photo.library_id })}`;
}

export default function GalleryLibrary({ active, canEdit }: Props) {
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [pending, setPending] = useState('');

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    setLoading(true); setError('');
    request(`/api/media/gallery/list?limit=48&offset=${offset}&include_deleted=${includeDeleted ? 1 : 0}`, { signal: controller.signal })
      .then(async response => {
        const next = await response.json() as Photo[];
        if (!controller.signal.aborted) { setPhotos(next); setMore(response.headers.get('X-Has-More') === 'true'); }
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [active, offset, includeDeleted, revision]);

  async function toggleTrash(photo: Photo) {
    if (!photo.deleted_at && !window.confirm(`将“${photo.filename}”移入相册废纸篓？`)) return;
    const key = `${photo.library_id}:${photo.id}`;
    setPending(key); setError('');
    try {
      await request(photoURL('trash', photo) + `&restore=${photo.deleted_at ? 1 : 0}`, { method: 'POST' });
      setRevision(value => value + 1);
    } catch (e) { setError((e as Error).message); }
    finally { setPending(''); }
  }

  return <section className="space-y-4">
    <div className="flex flex-wrap items-center gap-4">
      <button className={button} disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新相册</button>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={includeDeleted} onChange={event => { setIncludeDeleted(event.target.checked); setOffset(0); }} />包含废纸篓</label>
      <span role="status" className="text-sm text-muted">{loading ? '正在加载…' : photos.length ? `第 ${offset + 1}–${offset + photos.length} 项` : '当前页暂无照片'}</span>
    </div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
      {photos.map(photo => <article key={`${photo.library_id}:${photo.id}`} className="overflow-hidden rounded-lg border border-line bg-surface">
        <a href={photoURL('original', photo)} target="_blank" rel="noopener noreferrer">
          <img src={photoURL('thumbnail', photo)} className="aspect-square w-full object-cover" loading="lazy" decoding="async" alt={photo.filename} />
        </a>
        <div className="space-y-2 p-3"><p className="break-all text-sm">{photo.filename}</p><p className="text-xs text-muted">{photo.library_name} · {Math.max(0, photo.size_bytes / 1024).toFixed(0)} KiB{photo.deleted_at ? ' · 废纸篓' : ''}</p>
          {canEdit && !photo.read_only ? <button className={button} disabled={!!pending} onClick={() => void toggleTrash(photo)}>{pending === `${photo.library_id}:${photo.id}` ? '正在保存…' : photo.deleted_at ? '恢复' : '移入废纸篓'}</button> : null}
        </div>
      </article>)}
    </div>
    <div className="flex gap-3"><button className={button} disabled={loading || offset === 0} onClick={() => setOffset(value => Math.max(0, value - 48))}>上一页</button><button className={button} disabled={loading || !more} onClick={() => setOffset(value => value + 48)}>下一页</button></div>
  </section>;
}
