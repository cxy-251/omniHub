import { useEffect, useState } from 'react';
import { api } from './api';
import { button, primaryButton } from './ui';

type NetState = { ip: string; fingerprint: string };
type Home = {
  status: { home_only: boolean; serving: string; reason?: string; current: NetState; trusted: boolean };
  networks: { fingerprint: string; name: string }[];
};
const input = 'rounded-md border border-line bg-background px-3 py-2 text-sm';

type Wan = { enabled: boolean; running: boolean; connections: number; domain: string; binary: string; config: string; error?: string };
type Check = { domain: string; ok: boolean; other: boolean; error?: string };
const post = (path: string, body: unknown) => api<unknown>(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

// NetworkAdmin：局域网、广域网（cloudflared 隧道与域名）、只在可信网络上对外服务。
export default function NetworkAdmin() {
  return <div className="space-y-5"><LanSection /><WanSection /><HomeSection /></div>;
}

// LanSection：局域网开关与网址。关闭只能在这台设备自己的浏览器里做（服务端会拒绝局域网来的关闭请求）。
function LanSection() {
  const [lan, setLan] = useState<{ enabled: boolean; urls: string[] }>();
  const [error, setError] = useState(''), [pending, setPending] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    api<{ enabled: boolean; urls: string[] }>('/api/network/lan', { signal: c.signal }).then(setLan).catch((e: Error) => { if (!c.signal.aborted) setError(e.message); });
    return () => c.abort();
  }, []);
  async function toggle(enabled: boolean) {
    setPending(true); setError('');
    try { setLan(await post('/api/network/lan', { enabled }) as { enabled: boolean; urls: string[] }); } catch (e) { setError((e as Error).message); }
    finally { setPending(false); }
  }
  return <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
    <h2 className="text-lg">局域网</h2>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {lan ? <>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={lan.enabled} disabled={pending} onChange={e => void toggle(e.target.checked)} />允许局域网访问</label>
      <p className="text-sm text-muted">{lan.enabled ? '同一个 Wi-Fi 里的手机、电脑在浏览器打开下面的网址即可使用。' : '只有这台设备自己能访问。'}</p>
      {lan.enabled ? <ul className="space-y-1">{lan.urls.map(u => <li key={u} className="flex items-center gap-2"><span className="break-all font-mono text-sm">{u}</span>
        <button className={button} onClick={() => void navigator.clipboard?.writeText(u)}>复制</button></li>)}</ul> : null}
    </> : null}
  </section>;
}

// WanSection：广域网经 cloudflared 命名隧道从外网访问。域名可以改：服务端改隧道配置里的 hostname，
// 在 Cloudflare 上把域名指向隧道，再经域名读回运行标识，确认指向的是这台设备。
function WanSection() {
  const [wan, setWan] = useState<Wan>();
  const [domain, setDomain] = useState('');
  const [check, setCheck] = useState<Check>();
  const [error, setError] = useState(''), [pending, setPending] = useState(''), [checking, setChecking] = useState(false);
  const load = (signal?: AbortSignal) => api<Wan>('/api/network/wan', { signal }).then(w => { setWan(w); setDomain(d => d || w.domain); }).catch((e: Error) => { if (!signal?.aborted) setError(e.message); });
  useEffect(() => { const c = new AbortController(); void load(c.signal); return () => c.abort(); }, []);
  // 打开期间每 3 秒刷新连接状态
  useEffect(() => {
    if (!wan?.enabled) return;
    const t = setInterval(() => void load(), 3000);
    return () => clearInterval(t);
  }, [wan?.enabled]);
  async function run(label: string, fn: () => Promise<void>) {
    setPending(label); setError('');
    try { await fn(); } catch (e) { setError((e as Error).message); }
    finally { setPending(''); }
  }
  async function test(host?: string) {
    setChecking(true); setCheck(undefined);
    try { setCheck(await api<Check>(`/api/network/wan/check${host ? `?domain=${encodeURIComponent(host)}` : ''}`)); } catch (e) { setError((e as Error).message); }
    finally { setChecking(false); }
  }
  if (!wan) return <section className="rounded-lg border border-line bg-panel p-4"><h2 className="text-lg">广域网</h2>{error ? <p role="alert" className="text-danger">{error}</p> : <p className="text-muted">正在读取…</p>}</section>;
  const state = !wan.binary ? '这台设备上没有 cloudflared，不能从外网访问。'
    : !wan.domain ? `隧道配置里没有域名：${wan.config}`
    : !wan.enabled ? '关闭中：经隧道来的请求一律拒绝。'
    : wan.connections > 0 ? `已连通（${wan.connections} 条连接）：https://${wan.domain}`
    : wan.running ? `正在连接 Cloudflare…${wan.error ? `（${wan.error}）` : ''}` : `未运行${wan.error ? `：${wan.error}` : ''}`;
  const changed = domain.trim().toLowerCase() !== wan.domain;
  return <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
    <h2 className="text-lg">广域网</h2>
    <p className="text-sm text-muted">经 Cloudflare 隧道从外网访问（需要登录账号）。每台设备用自己的隧道和域名。</p>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={wan.enabled} disabled={!!pending || !wan.binary} onChange={e => void run('toggle', async () => { setWan(await post('/api/network/wan', { enabled: e.target.checked }) as Wan); })} />允许广域网访问</label>
    <p className={`text-sm ${wan.enabled && wan.connections > 0 ? 'text-[#3fb950]' : 'text-muted'}`}>{state}</p>
    {wan.binary ? <form className="flex flex-wrap items-end gap-2" onSubmit={e => { e.preventDefault(); void run('domain', async () => { setWan(await post('/api/network/wan', { domain: domain.trim() }) as Wan); await test(domain.trim()); }); }}>
      <label className="flex min-w-0 flex-1 flex-col gap-1 text-sm">域名<input className={input} value={domain} onChange={e => setDomain(e.target.value)} placeholder="例如 phone.example.com" maxLength={253} /></label>
      <button className={primaryButton} disabled={!!pending || !changed || !domain.trim()}>{pending === 'domain' ? '正在设置…' : '换成这个域名'}</button>
      <button type="button" className={button} disabled={checking || !domain.trim()} onClick={() => void test(domain.trim())}>{checking ? '正在检测…' : '检测'}</button>
    </form> : null}
    {changed && wan.binary ? <p className="text-sm text-muted">换域名会改这台设备的隧道配置（旧的存为 .bak），并在 Cloudflare 上把域名指向这条隧道；需要这台设备登录过 cloudflared（有 cert.pem）。</p> : null}
    {check ? <p className={`text-sm ${check.ok ? 'text-[#3fb950]' : 'text-danger'}`}>{check.ok ? `https://${check.domain} 能访问，指向这台设备。` : `https://${check.domain}：${check.error}`}</p> : null}
  </section>;
}

// HomeSection：只在可信网络（家里的 Wi-Fi）上对外服务。手机带出门时，局域网地址自动关闭，
// 本机浏览器（127.0.0.1）一直能用。读取状态时服务端会重新探测一次网络（约 2 秒）。
function HomeSection() {
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
