import { useCallback, useEffect, useState } from 'react';
import { api, post } from './api';

type Stats = { videos: number; videos_probed: number; albums: number; thumbnails: number; thumbnail_failures: number; albums_imported: number; albums_on_disk: number; albums_cleanable: number };
type Worker = { thumbnails_enabled: boolean; job: string; done: number; total: number; last_job: string; last_error: string; last_note: string };
type Status = { worker: Worker; libraries: { library_id: string; name: string; stats: Stats; error?: string }[] | null };

const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';
const jobNames: Record<string, string> = { scan: '扫描', import: '图集入库', clean: '清理已入库原目录', thumbnails: '生成缩略图' };

// ShortVideoAdmin 管理短视频索引：缩略图开关、扫描、图集入库与校验后清理原目录。
// 有任务运行时每 2 秒刷新进度，空闲时不轮询。
export default function ShortVideoAdmin() {
  const [status, setStatus] = useState<Status>();
  const [error, setError] = useState('');
  const [confirmClean, setConfirmClean] = useState(false);

  const load = useCallback((signal?: AbortSignal) => api<Status>('/api/media/shortvideo/admin/status', { signal })
    .then(next => { setStatus(next); setError(''); })
    .catch((e: Error) => { if (!signal?.aborted) setError(e.message); }), []);

  const busy = !!status?.worker.job;
  const thumbsRunning = status?.worker.thumbnails_enabled && status.libraries?.some(l => l.stats.thumbnails + l.stats.thumbnail_failures < l.stats.videos + l.stats.albums);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    if (!busy && !thumbsRunning) return () => controller.abort();
    const timer = setInterval(() => void load(controller.signal), 2000);
    return () => { controller.abort(); clearInterval(timer); };
  }, [load, busy, thumbsRunning]);

  async function act(action: string, body: unknown = {}) {
    try { await post(`/api/media/shortvideo/admin/${action}`, body); } catch (e) { setError((e as Error).message); }
    void load();
  }

  const w = status?.worker;
  const cleanable = status?.libraries?.reduce((n, l) => n + l.stats.albums_cleanable, 0) ?? 0;
  const importable = status?.libraries?.reduce((n, l) => n + l.stats.albums_on_disk - l.stats.albums_cleanable, 0) ?? 0;
  return <section className="space-y-4">
    <h2 className="text-xl font-semibold">短视频</h2>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    <div className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <label className="flex items-center gap-3"><input type="checkbox" checked={!!w?.thumbnails_enabled} onChange={e => void act('thumbnails', { enabled: e.target.checked })} />
        <span>生成缩略图</span></label>
      <p className="text-sm text-muted">开启后在后台用 ffmpeg 逐个生成（低优先级，480px 宽），存入各库 shortvideo/thumbnails.db。关闭后立即停止，只显示已有缩略图。</p>
      <div className="flex flex-wrap gap-2">
        <button className={button} disabled={busy} onClick={() => void act('scan')}>重新扫描</button>
        <button className={button} disabled={busy || importable === 0} onClick={() => void act('import')}>图集入库（{importable}）</button>
        <button className={button} disabled={busy || cleanable === 0} onClick={() => setConfirmClean(true)}>清理已入库原目录（{cleanable}）</button>
        {busy && w?.job !== 'thumbnails' ? <button className={button} onClick={() => void act('cancel')}>取消当前任务</button> : null}
      </div>
      {confirmClean ? <div className="rounded-md border border-primary p-3 text-sm">
        <p>逐个核对原目录与 albums.db 中的入库内容（文件名、数量、SHA-256），完全一致的目录移入系统回收站；有任何差异的保留不动。背景音乐 mp3 不受影响。</p>
        <div className="mt-2 flex gap-2"><button className={button} onClick={() => { setConfirmClean(false); void act('clean'); }}>校验并移入回收站</button><button className={button} onClick={() => setConfirmClean(false)}>取消</button></div>
      </div> : null}
      <p className="text-sm" role="status">
        {w?.job && w.job !== 'thumbnails' ? `正在${jobNames[w.job] ?? w.job}：${w.done}/${w.total}` : w?.last_job ? `上次${jobNames[w.last_job] ?? w.last_job}：${w.last_note || '完成'}` : '空闲'}
      </p>
      {w?.last_error ? <p className="whitespace-pre-wrap break-all text-sm text-danger">{w.last_error}</p> : null}
    </div>
    <div className="grid gap-3 md:grid-cols-2">{status?.libraries?.map(lib => <article key={lib.library_id} className="rounded-lg border border-line bg-panel p-4 text-sm">
      <h3 className="mb-2 font-semibold">{lib.name}</h3>
      {lib.error ? <p className="text-muted">{lib.error}</p> : <ul className="space-y-1">
        <li>视频 {lib.stats.videos}，图集 {lib.stats.albums}</li>
        <li>编码/尺寸已探测 {lib.stats.videos_probed} / {lib.stats.videos}（后台自动进行，用于竖屏与浏览器解码过滤）</li>
        <li>缩略图 {lib.stats.thumbnails} / {lib.stats.videos + lib.stats.albums}{lib.stats.thumbnail_failures ? `（${lib.stats.thumbnail_failures} 个源文件无法解码）` : ''}</li>
        <li>图集已入库 {lib.stats.albums_imported}，原目录仍在磁盘 {lib.stats.albums_on_disk}</li>
      </ul>}
    </article>)}</div>
  </section>;
}
