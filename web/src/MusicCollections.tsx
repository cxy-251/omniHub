import { useEffect, useMemo, useState } from 'react';
import { api, post } from './api';
import { useAudio, type Track } from './AudioEngine';
import { usePaged } from './paged';
import { chip } from './ui';
import { MusicRatingButtons } from './MusicFeedback';

type TrashRecord = { id: string; track_id: string; title: string; artist: string; created_at: string; status: string; error?: string };
const names: Record<string, string> = { all: '全部音乐', liked: '我的点赞', disliked: '不喜欢', other: '其他歌手', trash: '回收记录' };

export default function MusicCollections({ collection }: { collection: string }) {
  const engine = useAudio();
  const [page, setPage] = useState(0);
  const [query, setQuery] = useState('');
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [records, setRecords] = useState<TrashRecord[]>([]);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [message, setMessage] = useState('');
  const [revision, setRevision] = useState(0);
  const trash = collection === 'trash';
  const scope = `@music/${collection}`;
  const selectedIds = useMemo(() => Object.keys(selected).filter(id => selected[id]), [selected]);
  const paged = usePaged<Track>(trash ? '' : `/api/media/audio/tracks?${new URLSearchParams({ path: scope, ...(search ? { q: search } : {}), rev: String(engine?.musicVersion ?? 0) })}`, 100);
  useEffect(() => { setPage(0); setSelected({}); setMessage(''); }, [collection]);
  useEffect(() => { setSelected({}); }, [page]);
  useEffect(() => { const timer = window.setTimeout(() => { setSearch(query.trim()); setPage(0); }, 250); return () => window.clearTimeout(timer); }, [query]);
  useEffect(() => {
    if (!trash || !engine?.music.can_trash) return;
    const controller = new AbortController();
    api<TrashRecord[]>('/api/media/audio/music/trash', { signal: controller.signal }).then(setRecords)
      .catch((e: Error) => { if (!controller.signal.aborted) setMessage(e.message); });
    return () => controller.abort();
  }, [trash, engine?.music.can_trash, revision]);
  const refresh = async () => { setSelected({}); setRevision(v => v + 1); await engine?.refreshMusic(true); };
  const recycle = async () => {
    setBusy(true); setConfirm(false);
    try {
      const result = await post<{ moved: string[]; failed: { id: string; error: string }[] }>('/api/media/audio/music/trash', { ids: selectedIds });
      if (engine?.track && result.moved.includes(engine.track.id)) engine.stop(true);
      setMessage(`已移入系统回收站 ${result.moved.length} 首${result.failed.length ? `；失败 ${result.failed.length} 首：${result.failed[0].error}` : ''}`);
      await refresh();
    } catch (e) { setMessage((e as Error).message); } finally { setBusy(false); }
  };
  const restore = async (id: string) => {
    setBusy(true);
    try { await post('/api/media/audio/music/restore', { id }); setMessage('已恢复到原来的歌手／专辑目录'); await refresh(); }
    catch (e) { setMessage((e as Error).message); } finally { setBusy(false); }
  };
  const rows = trash ? records : Array.from({ length: Math.min(100, Math.max(0, paged.total - page * 100)) }, (_, i) => paged.item(page * 100 + i));
  return <div className="min-w-0 space-y-3">
    <div className="flex flex-wrap items-center gap-2">
      <h2 className="text-lg font-semibold">{names[collection]}</h2>
      <span className="text-sm text-muted">{trash ? `${records.length} 条` : `${paged.total} 首`}</span>
      {!trash ? <input className="min-w-0 flex-1 rounded border border-line bg-background px-2 py-1 text-sm md:ml-auto md:max-w-72" placeholder="搜索歌名、歌手、专辑" value={query} onChange={event => setQuery(event.target.value)} /> : null}
    </div>
    {collection === 'disliked' ? <>
      <p className="text-xs text-muted">不喜欢只作标记；选择后再回收，可在“回收记录”恢复。</p>
      {engine?.music.can_trash ? <div className="flex flex-wrap gap-2">
        <button className={chip(false)} disabled={busy} onClick={() => setSelected(Object.fromEntries((rows as (Track | undefined)[]).filter((t): t is Track => !!t).map(t => [t.id, true])))}>全选本页</button>
        <button className={chip(false)} onClick={() => setSelected({})}>取消选择</button>
        <button className="rounded bg-danger px-3 py-1 text-sm text-white disabled:opacity-40" disabled={busy || !selectedIds.length} onClick={() => setConfirm(true)}>移入系统回收站（{selectedIds.length}）</button>
      </div> : null}
    </> : null}
    {trash ? <p className="text-xs text-muted">恢复不覆盖已有文件；系统回收站清空后无法恢复。显示最近 2000 条记录。</p> : null}
    {message || paged.error ? <p role="status" className="break-words text-sm text-[#e3b341]">{message || paged.error}</p> : null}
    <div className="max-h-[65vh] overflow-y-auto rounded border border-line divide-y divide-line">
      {trash ? records.map(t => <div key={t.id} className="flex items-center gap-2 p-3">
        <div className="min-w-0 flex-1"><p className="truncate text-sm">{t.title}</p><p className="truncate text-xs text-muted">{t.artist} · {t.created_at.slice(0, 10)}</p>{t.error ? <p className="text-xs text-danger">{t.error}</p> : null}</div>
        <button className={chip(false)} disabled={busy} onClick={() => void restore(t.id)}>恢复</button>
      </div>) : (rows as (Track | undefined)[]).map((t, i) => <div key={t?.id ?? i} className={`flex flex-wrap items-center gap-2 p-2 ${t && engine?.track?.id === t.id ? 'bg-panel' : ''}`}>
        {collection === 'disliked' && engine?.music.can_trash ? <input type="checkbox" aria-label={`选择 ${t?.title ?? '歌曲'}`} checked={!!(t && selected[t.id])} disabled={!t || busy} onChange={event => t && setSelected(old => ({ ...old, [t.id]: event.target.checked }))} /> : null}
        <button className="min-w-0 flex-1 text-left" disabled={!t} onClick={() => t && engine?.open(scope, names[collection], t.index - 1, true)}>
          <span className="block truncate text-sm">{t?.title ?? '…'}</span><span className="block truncate text-xs text-muted">{t?.artist}{t?.album ? ` · ${t.album}` : ''}</span>
        </button>
        {t ? <MusicRatingButtons id={t.id} engine={engine} /> : null}
      </div>)}
      {!rows.length ? <p className="p-4 text-sm text-muted">暂无歌曲或记录</p> : null}
    </div>
    {!trash && paged.total > 100 ? <div className="flex items-center gap-3"><button className={chip(false)} disabled={!page} onClick={() => setPage(v => v - 1)}>上一页</button><span className="text-sm text-muted">{page + 1} / {Math.ceil(paged.total / 100)}</span><button className={chip(false)} disabled={(page + 1) * 100 >= paged.total} onClick={() => setPage(v => v + 1)}>下一页</button></div> : null}
    {confirm ? <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4" role="dialog" aria-modal="true" aria-labelledby="music-trash-title">
      <div className="w-full max-w-md space-y-4 rounded-lg border border-line bg-panel p-5"><h3 id="music-trash-title" className="font-semibold">将 {selectedIds.length} 首歌曲移入系统回收站？</h3><p className="text-sm text-muted">只回收所选音乐文件，专辑封面保留，可在回收记录中恢复。</p><div className="flex justify-end gap-2"><button className={chip(false)} onClick={() => setConfirm(false)}>取消</button><button className="rounded bg-danger px-3 py-1 text-sm text-white" onClick={() => void recycle()}>确认回收</button></div></div>
    </div> : null}
  </div>;
}
