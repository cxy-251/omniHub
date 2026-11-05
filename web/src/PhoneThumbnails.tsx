import { useState } from 'react';
import { api } from './api';
import { button, primaryButton } from './ui';

export type PhoneReview = { token: string; directories: number; files: number; bytes: number; skipped: number; trash: string; moved: boolean; remaining: number; can_clean: boolean };

// Only mounted on the host; credentials are used once and never stored.
export default function PhoneThumbnails({ busy, review, act }: { busy: boolean; review?: PhoneReview; act(action: string, body: unknown): Promise<void> }) {
  const [devices, setDevices] = useState<{ id: string; label: string }[]>([]);
  const [device, setDevice] = useState(''), [user, setUser] = useState('admin'), [password, setPassword] = useState(''), [port, setPort] = useState(8998), [error, setError] = useState('');
  const input = 'min-w-0 rounded border border-line bg-background px-2 py-1.5 text-sm';
  return <div className="space-y-2 border-t border-line pt-3">
    <h3 className="font-semibold">手机旧封面维护（ADB）</h3>
    <div className="flex flex-wrap gap-2">
      <select aria-label="ADB 手机" className={`${input} max-w-full flex-1`} value={device} onChange={e => setDevice(e.target.value)}><option value="">选择已授权的 USB／无线 ADB 手机</option>{devices.map(d => <option key={d.id} value={d.id}>{d.label}</option>)}</select>
      <button className={button} disabled={busy} onClick={() => void api<typeof devices>('/api/media/shortvideo/admin/phone-devices').then(list => { setDevices(list); setDevice(list[0]?.id ?? ''); setError(list.length ? '' : '没有已授权设备，请先连接无线 ADB 或 USB 调试'); }).catch((e: Error) => setError(e.message))}>读取已连接设备</button>
      <button className={button} disabled={busy || !device} onClick={() => void act('phone-thumbs-history', {device})}>读取回收记录</button>
    </div>
    <form className="flex flex-wrap gap-2" onSubmit={e => { e.preventDefault(); const body = { device, user, password, port }; setPassword(''); void act('phone-thumbs-plan', body); }}>
      <input aria-label="手机管理员" className={`${input} w-36`} value={user} onChange={e => setUser(e.target.value)} placeholder="手机 omniHub 管理员" required />
      <input aria-label="手机密码" className={`${input} w-44`} type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="手机密码（不保存）" autoComplete="off" required />
      <input aria-label="手机服务端口" className={`${input} w-24`} type="number" min={1} max={65535} value={port} onChange={e => setPort(Number(e.target.value))} required />
      <button className={primaryButton} disabled={busy || !device || !password}>核对旧封面</button>
    </form>
    <p className="text-xs text-muted">逐张确认手机服务有数据库封面，每个目录再读取一张实际图片；含其他文件或缺封面的 .thumbs 目录保留。USB／无线均可，大批量传视频仍走数据线。</p>
    {error ? <p role="alert" className="text-sm text-danger">{error}</p> : null}
    {review ? <div className="space-y-2 rounded border border-primary p-3 text-sm">
      <p>核对通过 {review.directories} 个目录、{review.files} 张（{(review.bytes / 1e9).toFixed(2)} GB），保留 {review.skipped} 个目录。</p>
      <p className="break-all">回收位置：{review.trash}</p><p className="text-muted">移入回收目录不会释放手机空间；可在此恢复。</p>
      <div className="flex flex-wrap gap-2"><button className={button} disabled={busy || !review.can_clean || review.remaining === 0} onClick={() => void act('phone-thumbs-clean', { token: review.token })}>回收已核对的旧封面</button><button className={button} disabled={busy || !review.moved} onClick={() => void act('phone-thumbs-restore', { token: review.token })}>恢复本次旧封面</button></div>
    </div> : null}
  </div>;
}
