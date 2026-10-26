import { useEffect, useState } from 'react';
import { api } from './api';
import DirectoryPicker from './DirectoryPicker';
import { button, primaryButton } from './ui';

type Library = { id: string; name: string; is_primary: boolean; is_online: boolean; read_only: boolean };
type Progress = { Total: number; Done: number; Added: number; Duplicates: number; Unsupported: number; Errors: string[] | null };
type ImportStatus = { running?: boolean; finished?: string; error?: string; source?: string; library?: string; progress?: Progress };

// GalleryAdmin：把服务端所在设备上的一个文件夹（含子文件夹）导入某个资源库的相册（与桌面端同一接口）。
// 原图写进 originals 里未满 4GB 的分卷，gallery.db 记照片、480px 缩略图和色系；内容相同的照片跳过，源文件夹不改动。
// 导入在服务端进行，离开页面不影响；进行中每秒刷新进度。
export default function GalleryAdmin() {
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [target, setTarget] = useState('');
  const [folder, setFolder] = useState(''), [picking, setPicking] = useState(false);
  const [status, setStatus] = useState<ImportStatus>({});
  const [error, setError] = useState('');
  const running = !!status.running;

  const refresh = (signal?: AbortSignal) => api<ImportStatus>('/api/media/gallery/admin/import', { signal }).then(setStatus).catch((e: Error) => { if (!signal?.aborted) setError(e.message); });
  useEffect(() => {
    const c = new AbortController();
    api<{ libraries: Library[] }>('/api/library/manage', { signal: c.signal }).then(s => {
      const writable = s.libraries.filter(l => l.is_online && !l.read_only);
      setLibraries(writable);
      setTarget((writable.find(l => l.is_primary) ?? writable[0])?.id ?? '');
    }).catch((e: Error) => { if (!c.signal.aborted) setError(e.message); });
    void refresh(c.signal);
    return () => c.abort();
  }, []);
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => void refresh(), 1000);
    return () => clearInterval(t);
  }, [running]);

  async function send(method: string, body?: unknown) {
    setError('');
    try { setStatus(await api<ImportStatus>('/api/media/gallery/admin/import', { method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })); }
    catch (e) { setError((e as Error).message); }
  }
  const p = status.progress;
  return <section className="space-y-4">
    <h2 className="text-xl font-semibold">相册</h2>
    <div className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <p className="text-sm text-muted">把文件夹里的照片（JPEG、PNG、WebP、GIF，含子文件夹）加进相册。文件夹在运行 omniHub 的那台设备上。已在相册里的同一张照片会跳过，源文件不会被移动或删除。HEIC 等其他格式暂不支持，会计入「格式不支持」。</p>
      {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
      {!libraries.length && !error ? <p className="text-sm text-muted">没有在线、可写的资源库：先在「资源库」里添加。</p> : null}
      <label className="flex flex-wrap items-center gap-2 text-sm">导入到
        <select className="rounded border border-line bg-background px-2 py-1" value={target} disabled={running} onChange={e => setTarget(e.target.value)}>
          {libraries.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}
        </select>
      </label>
      <div className="flex flex-wrap items-center gap-2">
        <button className={button} disabled={running} onClick={() => setPicking(v => !v)}>选择文件夹…</button>
        <span className={`min-w-0 break-all font-mono text-sm ${folder ? '' : 'text-muted'}`}>{folder || '未选择'}</span>
      </div>
      {picking ? <DirectoryPicker onPick={path => { setFolder(path); setPicking(false); }} onCancel={() => setPicking(false)} /> : null}
      <div className="flex flex-wrap gap-2">
        <button className={primaryButton} disabled={running || !folder || !target} onClick={() => void send('POST', { library_id: target, path: folder })}>开始导入</button>
        {running ? <button className={button} onClick={() => void send('DELETE')}>取消</button> : null}
      </div>
      {running && p ? <progress className="w-full" max={Math.max(1, p.Total)} value={p.Done} /> : null}
      {running || status.finished ? <p className={`whitespace-pre-line text-sm ${status.error ? 'text-danger' : running ? '' : 'text-[#3fb950]'}`}>
        {`${running ? '正在导入 ' : status.error ? `导入结束（${status.error}）：` : '导入完成：'}${status.source ?? ''} → ${status.library ?? ''}
已处理 ${p?.Done ?? 0} / ${p?.Total ?? 0}，新增 ${p?.Added ?? 0}，重复跳过 ${p?.Duplicates ?? 0}，格式不支持 ${p?.Unsupported ?? 0}${p?.Errors?.length ? `，出错 ${p.Errors.length}` : ''}`}
      </p> : null}
      {p?.Errors?.map((e, i) => <p key={i} className="break-all text-sm text-danger">{e}</p>)}
    </div>
  </section>;
}
