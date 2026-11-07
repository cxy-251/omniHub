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
type TlsInfo = {
  enabled: boolean;
  ca_exists: boolean;
  ca_fingerprint?: string;
  cert_expires_at?: string;
  sans: string[];
  extra_hosts: string[];
  https_urls: string[];
};
const post = (path: string, body: unknown) => api<unknown>(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

// NetworkAdmin：局域网、HTTPS 安全证书、广域网（cloudflared 隧道与域名）、只在可信网络上对外服务。
export default function NetworkAdmin() {
  return <div className="space-y-5"><LanSection /><HttpsSection /><WanSection /><HomeSection /></div>;
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

// HttpsSection：内置本地 Root CA 与多 IP/域名自动签发 HTTPS 证书（同端口自适应 HTTP/HTTPS）。
function HttpsSection() {
  const [tls, setTls] = useState<TlsInfo>();
  const [hostsInput, setHostsInput] = useState('');
  const [error, setError] = useState(''), [notice, setNotice] = useState(''), [pending, setPending] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    api<TlsInfo>('/api/network/tls', { signal: c.signal }).then(info => {
      setTls(info);
      setHostsInput((info.extra_hosts ?? []).join(', '));
    }).catch((e: Error) => { if (!c.signal.aborted) setError(e.message); });
    return () => c.abort();
  }, []);
  async function update(body: Record<string, unknown>, msg?: string) {
    setPending(true); setError(''); setNotice('');
    try {
      const next = await post('/api/network/tls', body) as TlsInfo;
      setTls(next);
      setHostsInput((next.extra_hosts ?? []).join(', '));
      if (msg) setNotice(msg);
    } catch (e) { setError((e as Error).message); }
    finally { setPending(false); }
  }
  return <section className="space-y-3 rounded-lg border border-line bg-panel p-4">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-lg">HTTPS 与安全证书（消除 Not Secure）</h2>
      {tls?.ca_fingerprint ? <span className="font-mono text-xs text-muted">CA 指纹: {tls.ca_fingerprint} · 有效期至 {tls.cert_expires_at}</span> : null}
    </div>
    <details className="settings-help"><summary>为什么显示 Not Secure 以及如何解决</summary><p>直接通过公网 IP、局域网 IP 或自建端口访问 HTTP 时，浏览器会标记「不安全（Not Secure）」。开启下方开关后，omniHub 会在原端口同时支持 HTTP 与 HTTPS，并由本机内置根证书（Root CA）自动签发包含本机所有 IP 与自定义域名/公网 IP 的服务器证书。只需在手机或电脑上下载并信任一次根证书，之后访问 https:// 链接即显示安全锁。</p></details>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {notice ? <p role="status" className="text-sm text-[#3fb950]">{notice}</p> : null}
    {tls ? <>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={tls.enabled} disabled={pending} onChange={e => void update({ enabled: e.target.checked }, e.target.checked ? '已开启同端口 HTTPS 支持' : '已关闭 HTTPS')} />
        启用内置 HTTPS 安全连接（与 HTTP 共用同一端口，不影响本机客户端）
      </label>
      <div className="flex flex-wrap items-center gap-2">
        <a className={primaryButton} href="/api/network/tls/ca.crt" download="omnihub-root-ca.crt">下载根证书 (.crt · 安卓/Windows/电脑)</a>
        <a className={button} href="/api/network/tls/ca.mobileconfig" download="omnihub-root-ca.mobileconfig">下载苹果描述文件 (.mobileconfig · iPhone/iPad/Mac)</a>
        <button type="button" className={button} disabled={pending} onClick={() => void update({ regenerate: true }, '已重新签发服务器证书')}>重新签发服务器证书</button>
      </div>
      <form className="flex flex-wrap items-end gap-2" onSubmit={e => {
        e.preventDefault();
        const list = hostsInput.split(/[,;\s]+/).map(s => s.trim()).filter(Boolean);
        void update({ extra_hosts: list, regenerate: true }, '已更新证书附加 IP/域名并重签证书');
      }}>
        <label className="flex min-w-0 flex-1 basis-full flex-col gap-1 text-sm sm:basis-auto">
          附加公网 IP / DDNS 域名 / Tailscale 地址（多个用逗号分隔）
          <input className={input} value={hostsInput} onChange={e => setHostsInput(e.target.value)} placeholder="例如 114.xx.xx.xx, home.example.com, 100.64.0.2" />
        </label>
        <button className={button} disabled={pending}>保存并重签证书</button>
      </form>
      {tls.enabled && tls.https_urls?.length ? <div className="space-y-1">
        <p className="text-xs text-muted">HTTPS 访问地址：</p>
        <ul className="space-y-1">{tls.https_urls.map(u => <li key={u} className="flex items-center gap-2"><span className="break-all font-mono text-sm text-[#3fb950]">{u}</span>
          <button className={button} onClick={() => void navigator.clipboard?.writeText(u)}>复制</button></li>)}</ul>
      </div> : null}
      <details className="settings-help"><summary>各设备信任根证书步骤（只需操作一次）</summary>
        <ul className="list-disc space-y-1 pl-5 text-xs text-muted">
          <li><strong>iPhone / iPad</strong>：点击「下载苹果描述文件」→ 打开系统「设置」顶部点击「已下载描述文件」安装 → 再进入「设置 → 通用 → 关于本机 → 证书信任设置」，打开「omniHub Local Root CA」开关。</li>
          <li><strong>Android</strong>：点击「下载根证书 (.crt)」→ 系统「设置 → 安全与隐私 → 更多安全设置 → 加密与凭据 → 安装证书 → CA 证书」选择下载的 .crt 文件。</li>
          <li><strong>Windows</strong>：双击下载的 .crt →「安装证书」→ 选择「本地计算机」→ 放入「受信任的根证书颁发机构」。</li>
          <li><strong>macOS / Linux</strong>：macOS 双击放入「系统」钥匙串并设为「始终信任」；Linux 将 .crt 复制到 /usr/local/share/ca-certificates/ 并执行 update-ca-certificates。</li>
        </ul>
      </details>
    </> : <p className="text-sm text-muted">正在读取证书状态…</p>}
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
