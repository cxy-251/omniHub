import { useEffect, useState } from 'react';
import { api } from './api';
import { button, primaryButton } from './ui';

type NetState = { ip: string; fingerprint: string };
type Home = {
  status: { home_only: boolean; serving: string; reason?: string; current: NetState; trusted: boolean };
  networks: { fingerprint: string; name: string }[];
};
const input = 'rounded-md border border-line bg-background px-3 py-2 text-sm';

// NetworkAdmin：只在可信网络（家里的 Wi-Fi）上对外服务。手机带出门时，局域网地址自动关闭，
// 本机浏览器（127.0.0.1）一直能用。读取状态时服务端会重新探测一次网络（约 2 秒）。
export default function NetworkAdmin() {
  const [home, setHome] = useState<Home>();
  const [name, setName] = useState('家里');
  const [error, setError] = useState(''), [pending, setPending] = useState(true);
  const load = (signal?: AbortSignal) => {
    setPending(true);
    return api<Home>('/api/network/home', { signal })
      .then(h => { setHome(h); setError(''); })
      .catch((e: Error) => { if (!signal?.aborted) setError(e.message); })
      .finally(() => { if (!signal?.aborted) setPending(false); });
  };
  useEffect(() => { const c = new AbortController(); void load(c.signal); return () => c.abort(); }, []);
  async function change(body: unknown) {
    setPending(true); setError('');
    try {
      // 回复是切换监听之前的状态；切换会断开旧连接，稍等再重新读取
      await api('/api/network/home', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      await new Promise(r => setTimeout(r, 500));
      await load();
    } catch (e) { setError((e as Error).message); setPending(false); }
  }
  const s = home?.status;
  return <section className="space-y-4 rounded-lg border border-line bg-panel p-4">
    <div>
      <h2 className="mb-1 text-lg">只在家里的网络上对外服务</h2>
      <p className="text-sm text-muted">打开后，局域网里的其他设备只有在可信网络上才能访问；连到别的 Wi-Fi 或用移动数据时自动停止对外服务，回到可信网络 15 秒内恢复。这台设备自己的浏览器（127.0.0.1）一直能用。可信网络按路由器的 UPnP 标识识别，路由器需要打开 UPnP。</p>
    </div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {s ? <>
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={s.home_only} disabled={pending} onChange={e => void change({ home_only: e.target.checked })} />只在可信网络上对外服务
        </label>
        <button className={button} disabled={pending} onClick={() => void load()}>重新检测</button>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted">当前网络</dt>
        <dd className="break-all">{s.current.ip ? `${s.current.ip} · ${s.current.fingerprint ? (s.trusted ? '可信' : '不是可信网络') : '读不到网络标识'}` : '没有连接局域网'}</dd>
        <dt className="text-muted">对外服务</dt>
        <dd className="break-all">{s.serving ? (s.serving === '*' || s.serving === '0.0.0.0' ? '所有地址' : s.serving) : `已停止${s.reason ? `：${s.reason}` : ''}`}</dd>
      </dl>
      {s.current.fingerprint && !s.trusted ? <form className="flex flex-wrap items-end gap-3" onSubmit={e => { e.preventDefault(); void change({ trust: name.trim() || '可信网络' }); }}>
        <label className="text-sm">名称 <input className={input} maxLength={32} value={name} onChange={e => setName(e.target.value)} /></label>
        <button className={primaryButton} disabled={pending}>把当前网络加为可信</button>
      </form> : null}
      <div className="space-y-2">
        <h3 className="font-semibold">可信网络</h3>
        {home.networks.length ? home.networks.map(n => <div key={n.fingerprint} className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-line p-3 text-sm">
          <span className="break-all">{n.name || '未命名'}{n.fingerprint === s.current.fingerprint ? ' · 当前网络' : ''}</span>
          <button className={button} disabled={pending} onClick={() => void change({ untrust: n.fingerprint })}>移除</button>
        </div>) : <p className="text-sm text-muted">还没有可信网络。</p>}
      </div>
    </> : pending ? <p role="status" className="text-muted">正在检测网络…</p> : null}
  </section>;
}
