import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api } from './api';

type Library = { id: string; name: string; root_path: string; is_primary: boolean; is_online: boolean; read_only: boolean };
type Snapshot = { libraries: Library[]; primary: Library | null; state_directory: string; state_ready: boolean; state_error: string; category_directories: string[]; disabled_categories: string[] | null };
type Preview = { confirmation_token: string; source: string; destination: string; message: string; expires_at: string };

const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

export default function LibraryManager({ onChanged }: { onChanged: (name?: string) => void }) {
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [path, setPath] = useState('');
  const [readOnly, setReadOnly] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [preview, setPreview] = useState<(Preview & { library_id: string })>();
  const [removing, setRemoving] = useState<Library>();
  const [directories, setDirectories] = useState('');
  const lifetime = useRef<AbortController | null>(null);
  const changed = useRef(onChanged);
  changed.current = onChanged;

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    api<Snapshot>('/api/library/manage', { signal: controller.signal }).then(next => {
      if (!controller.signal.aborted) { setSnapshot(next); setDirectories(next.category_directories.join('\n')); }
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, []);

  async function perform<T>(endpoint: string, body: unknown, receive: (value: T) => void) {
    const signal = lifetime.current?.signal;
    if (pending || signal?.aborted) return;
    setPending(true); setError('');
    try {
      const value = await api<T>(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal });
      if (!signal?.aborted) receive(value);
    } catch (e) { if (!signal?.aborted) setError((e as Error).message); }
    finally { if (!signal?.aborted) setPending(false); }
  }

  function update(next: Snapshot) { setSnapshot(next); setDirectories(next.category_directories.join('\n')); changed.current(next.primary?.name); setPreview(undefined); setRemoving(undefined); }
  function add(event: FormEvent) {
    event.preventDefault();
    void perform<Snapshot>('/api/library/add', { path, read_only: readOnly }, next => { update(next); setPath(''); });
  }

  return <section className="space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-xl font-semibold">资源库管理</h2><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/refresh', {}, update)}>重新检测与恢复</button></div>
    <p className="text-sm text-muted">挂载和移除只改变配置。主库迁移会暂停当前播放；请先停止运行中的资源任务。库内媒体与模块数据库保留在各自物理库中。</p>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {snapshot ? <div className="rounded-lg border border-line bg-panel p-4"><p>全局状态：{snapshot.state_ready ? '可用' : '离线，等待原库恢复'}</p><p className="mt-2 break-all text-sm text-muted">{snapshot.state_directory}</p>{snapshot.state_error ? <p className="mt-2 text-danger">{snapshot.state_error}</p> : null}</div> : <p role="status">正在读取配置…</p>}
    <form className="flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4" onSubmit={add}>
      <label className="flex min-w-0 flex-1 flex-col gap-2 text-sm">物理资源库绝对路径<input className="rounded-md border border-line bg-background px-3 py-2" value={path} onChange={e => setPath(e.target.value)} required maxLength={4096} placeholder="选择服务端机器上的资源目录" /></label>
      <label className="flex items-center gap-2 py-2 text-sm"><input type="checkbox" checked={readOnly} onChange={e => setReadOnly(e.target.checked)} />只读挂载</label>
      <button className={button} disabled={pending}>添加资源库</button>
    </form>
    <div className="grid gap-3">{snapshot?.libraries.map(lib => <article key={lib.id} className="rounded-lg border border-line bg-panel p-4">
      <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">{lib.name} · {lib.is_online ? '在线' : '离线'}{lib.is_primary ? ' · 主库' : ''}{lib.read_only ? ' · 只读' : ''}</h3>
        <div className="flex gap-2"><button className={button} disabled={pending || !lib.is_online || lib.read_only || !snapshot.state_ready || snapshot.state_directory.replaceAll('\\', '/') === `${lib.root_path.replaceAll('\\', '/')}/.omni_data`} onClick={() => void perform<Preview>('/api/library/primary/preview', { library_id: lib.id }, next => { setPreview({ ...next, library_id: lib.id }); setRemoving(undefined); })}>设为主库</button><button className={button} disabled={pending} onClick={() => { setRemoving(lib); setPreview(undefined); }}>移除挂载</button></div></div>
      <p className="mt-2 break-all text-sm text-muted">{lib.root_path}</p>
    </article>)}</div>
    <form className="space-y-3 rounded-lg border border-line bg-panel p-4" onSubmit={event => { event.preventDefault(); void perform<Snapshot>('/api/library/categories', { directories: directories.split('\n').map(dir => dir.trim()).filter(Boolean) }, update); }}>
      <h3 className="font-semibold">启用的分类目录</h3>
      <p className="text-sm text-muted">每行一个分类根目录，如 media_library/audio。新增分类在在线可写库中补建空目录；从清单移除分类会停止访问并保留文件，只读库不补建。空清单停用全部分类。</p>
      <textarea className="min-h-48 w-full rounded-md border border-line bg-background p-3 font-mono text-sm" aria-label="分类目录清单" value={directories} onChange={event => setDirectories(event.target.value)} maxLength={10000} />
      {snapshot?.disabled_categories?.length ? <p className="break-all text-sm text-muted">已停用：{snapshot.disabled_categories.join('、')}</p> : null}
      <button className={button} disabled={pending || !snapshot}>保存分类配置</button>
    </form>
    {preview ? <div role="dialog" aria-modal="false" aria-labelledby="migration-title" className="rounded-lg border border-primary bg-panel p-5">
      <h3 id="migration-title" className="font-semibold">确认迁移全局数据库</h3><p className="my-3 text-sm">{preview.message}</p><p className="break-all text-sm">从：{preview.source}<br />到：{preview.destination}</p>
      <div className="mt-4 flex gap-3"><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/primary/commit', { library_id: preview.library_id, confirmation_token: preview.confirmation_token }, update)}>确认迁移</button><button className={button} disabled={pending} onClick={() => setPreview(undefined)}>取消</button></div>
    </div> : null}
    {removing ? <div role="dialog" aria-labelledby="remove-title" className="rounded-lg border border-line bg-panel p-5"><h3 id="remove-title" className="font-semibold">移除 {removing.name} 的挂载？</h3><p className="my-3 text-sm">文件保留原位。全局状态仍位于原目录；若随后拔出该介质，需要重新接入才能恢复账号服务。</p><div className="flex gap-3"><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/remove', { library_id: removing.id }, update)}>确认移除</button><button className={button} disabled={pending} onClick={() => setRemoving(undefined)}>取消</button></div></div> : null}
    {pending ? <p role="status" className="text-sm text-muted">正在处理，请保持原库和目标库连接…</p> : null}
  </section>;
}
