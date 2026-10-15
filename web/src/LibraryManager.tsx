import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api } from './api';
import DirectoryPicker from './DirectoryPicker';

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
  const [picking, setPicking] = useState(false);
  const [newCategory, setNewCategory] = useState('');
  const lifetime = useRef<AbortController | null>(null);
  const changed = useRef(onChanged);
  changed.current = onChanged;

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    api<Snapshot>('/api/library/manage', { signal: controller.signal }).then(next => {
      if (!controller.signal.aborted) setSnapshot(next);
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

  function update(next: Snapshot) { setSnapshot(next); changed.current(next.primary?.name); setPreview(undefined); setRemoving(undefined); }
  function saveCategories(active: string[]) { void perform<Snapshot>('/api/library/categories', { directories: active }, update); }
  const categories = snapshot ? [...new Set([...snapshot.category_directories, ...(snapshot.disabled_categories ?? [])])].sort() : [];
  function add(event: FormEvent) {
    event.preventDefault();
    void perform<Snapshot>('/api/library/add', { path, read_only: readOnly }, next => { update(next); setPath(''); });
  }

  return <section className="space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-xl font-semibold">资源库</h2><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/refresh', {}, update)}>重新检测与恢复</button></div>
    <p className="text-sm text-muted">添加、移除立即生效，只改变配置，不动库内文件。主库迁移会暂停当前播放。</p>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {snapshot ? <div className="rounded-lg border border-line bg-panel p-4"><p>全局状态：{snapshot.state_ready ? '可用' : '离线，等待原库恢复'}</p><p className="mt-2 break-all text-sm text-muted">{snapshot.state_directory}</p>{snapshot.state_error ? <p className="mt-2 text-danger">{snapshot.state_error}</p> : null}</div> : <p role="status">正在读取配置…</p>}
    <form className="flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4" onSubmit={add}>
      <label className="flex min-w-0 flex-1 flex-col gap-2 text-sm">资源库根目录<input className="rounded-md border border-line bg-background px-3 py-2" value={path} onChange={e => setPath(e.target.value)} required maxLength={4096} placeholder="点「浏览」选择，或输入绝对路径" /></label>
      <button type="button" className={button} onClick={() => setPicking(value => !value)}>浏览…</button>
      <label className="flex items-center gap-2 py-2 text-sm"><input type="checkbox" checked={readOnly} onChange={e => setReadOnly(e.target.checked)} />只读挂载</label>
      <button className={button} disabled={pending || !path}>添加资源库</button>
    </form>
    {picking ? <DirectoryPicker onPick={picked => { setPath(picked); setPicking(false); }} onCancel={() => setPicking(false)} /> : null}
    <div className="grid gap-3">{snapshot?.libraries.map(lib => <article key={lib.id} className="rounded-lg border border-line bg-panel p-4">
      <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">{lib.name} · {lib.is_online ? '在线' : '离线'}{lib.is_primary ? ' · 主库' : ''}{lib.read_only ? ' · 只读' : ''}</h3>
        <div className="flex gap-2"><button className={button} disabled={pending || !lib.is_online || lib.read_only || !snapshot.state_ready || snapshot.state_directory.replaceAll('\\', '/') === `${lib.root_path.replaceAll('\\', '/')}/.omni_data`} onClick={() => void perform<Preview>('/api/library/primary/preview', { library_id: lib.id }, next => { setPreview({ ...next, library_id: lib.id }); setRemoving(undefined); })}>设为主库</button><button className={button} disabled={pending} onClick={() => { setRemoving(lib); setPreview(undefined); }}>移除挂载</button></div></div>
      <p className="mt-2 break-all text-sm text-muted">{lib.root_path}</p>
    </article>)}</div>
    <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <h3 className="font-semibold">分类目录</h3>
      <p className="text-sm text-muted">关闭即停用：停止扫描和展示，文件保持原样，随时可重新开启。开启的分类会在在线可写库中补建空目录。</p>
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{categories.map(dir => {
        const active = snapshot!.category_directories.includes(dir);
        return <label key={dir} className="flex items-center gap-2 text-sm"><input type="checkbox" disabled={pending} checked={active} onChange={() => saveCategories(active ? snapshot!.category_directories.filter(d => d !== dir) : [...snapshot!.category_directories, dir])} /><span className={active ? '' : 'text-muted'}>{dir}</span></label>;
      })}</div>
      <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); const dir = newCategory.trim(); if (dir && snapshot) { saveCategories([...snapshot.category_directories.filter(d => d !== dir), dir]); setNewCategory(''); } }}>
        <input className="rounded-md border border-line bg-background px-3 py-2 text-sm" value={newCategory} onChange={e => setNewCategory(e.target.value)} placeholder="新增分类，如 media_library/podcast" maxLength={128} />
        <button className={button} disabled={pending || !newCategory.trim()}>新增分类</button>
      </form>
    </section>
    {preview ? <div role="dialog" aria-modal="false" aria-labelledby="migration-title" className="rounded-lg border border-primary bg-panel p-5">
      <h3 id="migration-title" className="font-semibold">确认迁移全局数据库</h3><p className="my-3 text-sm">{preview.message}</p><p className="break-all text-sm">从：{preview.source}<br />到：{preview.destination}</p>
      <div className="mt-4 flex gap-3"><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/primary/commit', { library_id: preview.library_id, confirmation_token: preview.confirmation_token }, update)}>确认迁移</button><button className={button} disabled={pending} onClick={() => setPreview(undefined)}>取消</button></div>
    </div> : null}
    {removing ? <div role="dialog" aria-labelledby="remove-title" className="rounded-lg border border-line bg-panel p-5"><h3 id="remove-title" className="font-semibold">移除 {removing.name} 的挂载？</h3><p className="my-3 text-sm">文件保留原位。全局状态仍位于原目录；若随后拔出该介质，需要重新接入才能恢复账号服务。</p><div className="flex gap-3"><button className={button} disabled={pending} onClick={() => void perform<Snapshot>('/api/library/remove', { library_id: removing.id }, update)}>确认移除</button><button className={button} disabled={pending} onClick={() => setRemoving(undefined)}>取消</button></div></div> : null}
    {pending ? <p role="status" className="text-sm text-muted">正在处理，请保持原库和目标库连接…</p> : null}
  </section>;
}
