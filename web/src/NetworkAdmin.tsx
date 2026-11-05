import { useVisiblePolling } from './useVisiblePolling';
import { useEffect, useState } from 'react';
import { api } from './api';
import { button, primaryButton } from './ui';

type NetState = { ip: string; fingerprint: string };
type Home = {
  status: { home_only: boolean; serving: string; reason?: string; current: NetState; trusted: boolean };
  networks: { fingerprint: string; name: string }[];
  ssh: { running: boolean; key_only: boolean } | null; // 只在 Termux（手机）里有
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

const tunnelRole = <details className="settings-help"><summary>外网访问说明</summary><p>被访问的设备需运行 cloudflared 并配置自己的隧道和域名；浏览器访问方无需安装。外网访问需要登录账号。</p></details>;

// WanSection：广域网经 cloudflared 命名隧道从外网访问。域名可以改：服务端改隧道配置里的 hostname，
// 在 Cloudflare 上把域名指向隧道，再经域名读回运行标识，确认指向的是这台设备。
function WanSection() {
  const [wan, setWan] = useState<Wan>();
  const [domain, setDomain] = useState('');
  const [check, setCheck] = useState<Check>();
  const [error, setError] = useState(''), [pending, setPending] = useState(''), [checking, setChecking] = useState(false);
  const load = (signal?: AbortSignal) => api<Wan>('/api/network/wan', { signal }).then(w => { setWan(w); setDomain(d => d || w.domain); }).catch((e: Error) => { if (!signal?.aborted) setError(e.message); });
  useVisiblePolling(load, wan?.enabled ? 3000 : null);
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
  const state = !wan.domain ? `隧道配置里没有域名：${wan.config}`
    : !wan.enabled ? '关闭中：经隧道来的请求一律拒绝。'
    : wan.connections > 0 ? `已连通（${wan.connections} 条连接）：https://${wan.domain}`
    : wan.running ? `正在连接 Cloudflare…${wan.error ? `（${wan.error}）` : ''}` : `未运行${wan.error ? `：${wan.error}` : ''}`;
  const changed = domain.trim().toLowerCase() !== wan.domain;
  if (!wan.binary) return <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
    <h2 className="text-lg">广域网</h2>
    <p className="text-sm">未安装 cloudflared，外网访问不可用；本机和局域网仍可使用。</p>
    <details className="settings-help"><summary>安装与配置</summary><p>把 cloudflared 放在 ~/Applications/cloudflared/cloudflared 或系统 PATH 中（Termux：pkg install cloudflared）。在这台设备上登录 Cloudflare，配置命名隧道（~/.cloudflared/config.yml），重新打开此页填写域名，无需重启 omniHub。浏览器访问方无需安装。</p></details>
  </section>;
  return <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
    <h2 className="text-lg">广域网</h2>
    {tunnelRole}
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={wan.enabled} disabled={!!pending} onChange={e => void run('toggle', async () => { setWan(await post('/api/network/wan', { enabled: e.target.checked }) as Wan); })} />允许广域网访问</label>
    <p className={`break-all text-sm ${wan.enabled && wan.connections > 0 ? 'text-[#3fb950]' : 'text-muted'}`}>{state}</p>
    {<form className="flex flex-wrap items-end gap-2" onSubmit={e => { e.preventDefault(); void run('domain', async () => { setWan(await post('/api/network/wan', { domain: domain.trim() }) as Wan); await test(domain.trim()); }); }}>
      <label className="flex min-w-0 flex-1 basis-full flex-col gap-1 text-sm sm:basis-auto">域名<input className={input} value={domain} onChange={e => setDomain(e.target.value)} placeholder="例如 phone.example.com" maxLength={253} /></label>
      <button className={primaryButton} disabled={!!pending || !changed || !domain.trim()}>{pending === 'domain' ? '正在设置…' : '换成这个域名'}</button>
      <button type="button" className={button} disabled={checking || !domain.trim()} onClick={() => void test(domain.trim())}>{checking ? '正在检测…' : '检测'}</button>
    </form>}
    {changed ? <p className="text-sm text-muted">换域名会改这台设备的隧道配置（旧的存为 .bak），并在 Cloudflare 上把域名指向这条隧道；需要这台设备登录过 cloudflared（有 cert.pem）。</p> : null}
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
      <h2 className="mb-1 text-lg">可信网络</h2>
      <details className="settings-help"><summary>访问规则</summary><p>开启后只在可信网络对外服务；切换到其他网络时停止，返回后 15 秒内恢复。本机访问不受影响。通过路由器 UPnP 标识识别网络，需开启路由器 UPnP。</p></details>
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
      {home.ssh ? <p className={`text-sm ${home.ssh.running && !home.ssh.key_only ? 'text-danger' : 'text-muted'}`}>
        SSH（8022 端口，用来从电脑更新 omniHub）{home.ssh.running ? '正在运行' : '没有运行'}，{home.ssh.key_only ? '只允许密钥登录、没有密码' : '还允许密码或键盘交互登录'}。它不归这个开关管，在外面的 Wi-Fi 上也开着{home.ssh.key_only ? '；只认密钥，外人进不来' : '：请按安装说明改成只允许密钥登录'}。能登录的密钥记在 Termux 的 ~/.ssh/authorized_keys。
      </p> : null}
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
