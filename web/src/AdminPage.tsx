import { useEffect, useRef, useState } from 'react';
import { api, request, type Status, type User } from './api';

type Device = { id: string; user_id: number; username: string; device_name: string; ip: string; created_at: string; expires_at: string };
type Devices = { devices: Device[]; current_device_id: string; can_enroll: boolean; lan: boolean; capacity: number; lifetime_days: number };
const sections = [['gallery', '相册'], ['shortvideo', '短视频'], ['text', '文本'], ['audio', '音频'], ['manga', '漫画'], ['games', '游戏'], ['tools', '工具'], ['tasks', '任务'], ['admin', '管理']] as const;
const levels = [0, 1, 2, 3, 4, 99];
const input = 'rounded-md border border-line bg-background px-3 py-2 text-sm';
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

// AdminPage renders untrusted account/device labels as text and loads data only
// on entry or explicit changes. Credentials remain in HttpOnly browser cookies.
export default function AdminPage() {
  const [offset, setOffset] = useState(0), [more, setMore] = useState(false), [currentID, setCurrentID] = useState(0);
  const [username, setUsername] = useState(''), [password, setPassword] = useState(''), [role, setRole] = useState('user'), [newLevel, setNewLevel] = useState(1);
  const [passwordUser, setPasswordUser] = useState<number>(), [replacement, setReplacement] = useState('');
  const [users, setUsers] = useState<(User & { status: string })[]>([]);
  const [permissions, setPermissions] = useState<Record<string, number>>({});
  const [devices, setDevices] = useState<Devices>();
  const [name, setName] = useState(''), [revision, setRevision] = useState(0);
  const [error, setError] = useState(''), [notice, setNotice] = useState(''), [pending, setPending] = useState(false);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    return () => { controller.abort(); lifetime.current = null; };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([api<Record<string, number>>('/api/admin/permissions', { signal: controller.signal }), request(`/api/admin/users?limit=32&offset=${offset}`, { signal: controller.signal }).then(async response => ({ rows: await response.json() as (User & { status: string })[], more: response.headers.get('X-Has-More') === 'true' })), api<Devices>('/api/admin/devices', { signal: controller.signal }), api<Status>('/api/auth/status', { signal: controller.signal })])
      .then(([matrix, accounts, remembered, status]) => { setPermissions(matrix); setUsers(accounts.rows); setMore(accounts.more); setCurrentID(status.user.id); setDevices(remembered); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [revision, offset]);

  async function change(path: string, body?: unknown, method = 'POST', reload = false) {
    const signal = lifetime.current?.signal; if (!signal || pending) return;
    setPending(true); setError(''); setNotice('');
    try {
      await api(path, { method, signal, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
      if (reload) { window.location.reload(); return true; }
      setNotice('已更新，相关凭证的撤销会在下一请求生效。'); setRevision(value => value + 1); return true;
    } catch (e) { if (!signal.aborted) setError((e as Error).message); return false; }
    finally { if (!signal.aborted) setPending(false); }
  }
  return <div className="space-y-6">
    <div className="flex items-center gap-3"><button className={button} disabled={pending} onClick={() => { setError(''); setRevision(value => value + 1); }}>刷新管理状态</button>{notice ? <p role="status" className="text-muted">{notice}</p> : null}</div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    <section className="rounded-lg border border-line bg-panel p-4"><h2 className="mb-4 text-lg">分类准入等级</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{sections.map(([id, label]) => <div key={id} className="flex items-center justify-between gap-2"><label>{label} <select className={input} value={permissions[id] ?? 0} onChange={e => setPermissions(old => ({ ...old, [id]: Number(e.target.value) }))}>{levels.map(level => <option key={level} value={level}>{level === 99 ? 'Admin' : `等级 ${level}`}</option>)}</select></label><button className={button} disabled={pending || !devices} onClick={() => void change('/api/admin/permissions', { section_id: id, required_level: permissions[id] ?? 0 })}>保存</button></div>)}</div>
    </section>
    <section className="rounded-lg border border-line bg-panel p-4"><h2 className="mb-4 text-lg">创建用户</h2>
      <form className="flex flex-wrap items-end gap-3" onSubmit={event => { event.preventDefault(); void change('/api/admin/users/create', { username: username.trim(), password, role, level: role === 'admin' ? 99 : newLevel }).then(ok => { setPassword(''); if (ok) setUsername(''); }); }}>
        <label>用户名 <input className={input} required maxLength={64} value={username} onChange={e => setUsername(e.target.value)} autoComplete="off" /></label>
        <label>初始密码 <input className={input} type="password" required maxLength={72} value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" /></label>
        <label>角色 <select className={input} value={role} onChange={e => setRole(e.target.value)}><option value="user">普通用户</option><option value="admin">管理员</option></select></label>
        {role === 'user' ? <label>等级 <select className={input} value={newLevel} onChange={e => setNewLevel(Number(e.target.value))}>{levels.filter(value => value !== 99).map(value => <option key={value}>{value}</option>)}</select></label> : null}
        <button className={button} disabled={pending || !devices}>创建</button>
      </form>
    </section>
    <section className="rounded-lg border border-line bg-panel p-4"><h2 className="mb-4 text-lg">用户等级与状态</h2><p className="mb-3 text-sm text-muted">修改会撤销该账号的普通会话与信任设备。管理员等级固定为 99。</p>
      <div className="space-y-3">{users.map(user => <div key={user.id} className="flex flex-wrap items-center gap-3"><span className="min-w-40 break-all">{user.username} · {user.role} · {user.status}</span><select aria-label={`${user.username} 的等级`} className={input} disabled={user.role === 'admin'} value={user.level} onChange={e => setUsers(old => old.map(item => item.id === user.id ? { ...item, level: Number(e.target.value) } : item))}>{levels.map(level => <option key={level}>{level}</option>)}</select><button className={button} disabled={pending || user.role === 'admin'} onClick={() => void change('/api/admin/users', { user_id: user.id, level: user.level })}>保存并撤销凭证</button><button className={button} disabled={pending} onClick={() => void change('/api/admin/users/status', { user_id: user.id, status: user.status === 'active' ? 'disabled' : 'active' }, 'POST', user.id === currentID)}>{user.status === 'active' ? '停用账号' : '恢复账号'}</button><button className={button} disabled={pending} onClick={() => { setPasswordUser(user.id); setReplacement(''); }}>重置密码</button></div>)}</div>
      <div className="mt-4 flex gap-3"><button className={button} disabled={pending || offset === 0} onClick={() => setOffset(value => Math.max(0, value - 32))}>上一页</button><button className={button} disabled={pending || !more} onClick={() => setOffset(value => value + 32)}>下一页</button></div>
      {passwordUser !== undefined ? <form className="mt-4 flex flex-wrap items-center gap-3" onSubmit={event => { event.preventDefault(); void change('/api/admin/users/password', { user_id: passwordUser, password: replacement }, 'POST', passwordUser === currentID).then(ok => { setReplacement(''); if (ok) setPasswordUser(undefined); }); }}><label>账号 #{passwordUser} 的新密码 <input className={input} type="password" required maxLength={72} autoComplete="new-password" value={replacement} onChange={e => setReplacement(e.target.value)} /></label><button className={button} disabled={pending}>更新并撤销全部凭证</button><button type="button" className={button} onClick={() => { setPasswordUser(undefined); setReplacement(''); }}>取消</button></form> : null}
    </section>
    <section className="rounded-lg border border-line bg-panel p-4"><h2 className="mb-3 text-lg">局域网信任设备</h2>
      <p className="mb-3 text-sm text-muted">信任当前浏览器 {devices?.lifetime_days ?? 180} 天，仅在直接 RFC1918 IPv4 局域网访问时免登录。登记 IP 用于辨认，不作为设备身份；地址变化后凭证仍可在局域网使用。公网、隧道与代理不会使用此凭证。</p>
      <div className="mb-4 flex flex-wrap items-center gap-3"><label>设备名称 <input className={input} maxLength={128} value={name} onChange={e => setName(e.target.value)} placeholder="例如：客厅平板" /></label><button className={button} disabled={pending || !devices?.can_enroll || !name.trim()} onClick={() => void change('/api/admin/devices', { name }, 'POST', true)}>信任当前浏览器</button></div>
      {!devices?.can_enroll ? <p className="mb-3 text-sm text-muted">登记需要通过局域网私有 IPv4 地址访问并使用账号密码登录；已信任的设备需重新密码登录后才能登记新凭证。</p> : null}
      <div className="space-y-3">{devices?.devices.map(device => <div key={device.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-line p-3"><div className="min-w-0"><p className="break-all">{device.device_name}{device.id === devices.current_device_id ? ' · 当前浏览器' : ''}</p><p className="text-sm text-muted">{device.username} · 登记 IP {device.ip} · 到期 {new Date(device.expires_at).toLocaleString()}</p></div><button className={button} disabled={pending} onClick={() => void change(`/api/admin/devices?id=${encodeURIComponent(device.id)}`, undefined, 'DELETE', device.id === devices.current_device_id)}>撤销信任</button></div>)}</div>
      {devices && !devices.devices.length ? <p className="text-muted">尚无已登记设备。</p> : null}
    </section>
  </div>;
}
