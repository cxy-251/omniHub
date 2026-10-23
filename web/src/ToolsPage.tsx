import { useEffect, useRef, useState } from 'react';
import { api, post, request } from './api';
import { button, chip, primaryButton, select, store, stored } from './ui';

// 局域网小工具：剪贴板、共享文件（media_library/shared）、硬件看板、测速、唤醒与局域网扫描。
// 所有内容都以文本节点渲染（旧版用 innerHTML 拼接剪贴板和文件名，别的设备能注入脚本）。
type Clip = { id: number; content: string; created_at: string };
type Shared = { name: string; size_bytes: number; mod_time: string; is_dir: boolean };
type Metrics = { os: string; num_cpu: number; mem_total_bytes: number; mem_avail_bytes: number; disk_free_bytes: number; disk_total_bytes: number };
type Device = { ip: string; mac: string; hostname: string; device: string };

const card = 'space-y-2 rounded-lg border border-line bg-panel p-3';
const size = (n: number) => n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`;

// 页面可见时才轮询。
function usePoll(fn: () => void, ms: number) {
  const latest = useRef(fn);
  latest.current = fn;
  useEffect(() => {
    latest.current();
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') latest.current(); }, ms);
    return () => window.clearInterval(t);
  }, [ms]);
}

// 每个工具单独分级（设置 → 子分区 tools/…），这里只显示当前账号能用的。
const tools = [['clipboard', '📋 剪贴板'], ['shared', '📁 共享文件'], ['status', '📊 主机状态'], ['network', '🌐 网络']] as const;

export default function ToolsPage({ sections }: { sections: string[] }) {
  const available = tools.filter(([id]) => sections.includes(`tools/${id}`));
  const [tab, setTab] = useState(() => stored('omnihub.tools.tab', 'clipboard'));
  useEffect(() => store('omnihub.tools.tab', tab), [tab]);
  const current = available.some(([id]) => id === tab) ? tab : available[0]?.[0];
  return <section className="space-y-3">
    <div className="flex flex-wrap gap-1.5">
      {available.map(([id, label]) => <button key={id} className={chip(current === id)} onClick={() => setTab(id)}>{label}</button>)}
    </div>
    <div className="max-w-3xl">
      {current === 'clipboard' ? <Clipboard /> : null}
      {current === 'shared' ? <SharedFiles /> : null}
      {current === 'status' ? <MetricsCard /> : null}
      {current === 'network' ? <Network /> : null}
    </div>
  </section>;
}

function Clipboard() {
  const [items, setItems] = useState<Clip[]>([]);
  const [text, setText] = useState('');
  const [msg, setMsg] = useState('');
  const load = () => { api<Clip[]>('/api/tools/clipboard').then(setItems).catch((e: Error) => setMsg(e.message)); };
  usePoll(load, 3000);
  const send = () => {
    if (!text.trim()) return;
    post('/api/tools/clipboard', { content: text }).then(() => { setText(''); setMsg(''); load(); }).catch((e: Error) => setMsg(e.message));
  };
  const copy = (s: string) => {
    // 局域网 HTTP 下 navigator.clipboard 不可用，退回到选中文本 + execCommand。
    const done = () => setMsg('已复制');
    if (navigator.clipboard && window.isSecureContext) { navigator.clipboard.writeText(s).then(done, () => setMsg('复制失败')); return; }
    const area = document.createElement('textarea');
    area.value = s; document.body.appendChild(area); area.select();
    try { document.execCommand('copy'); done(); } catch { setMsg('复制失败，请手动选择'); }
    area.remove();
  };
  return <div className={card}>
    <h3 className="font-semibold">📋 剪贴板 <span className="text-xs font-normal text-muted">各设备之间传文字和链接（服务重启后清空）</span></h3>
    <div className="flex gap-2">
      <textarea className={`${select} min-h-10 flex-1`} rows={2} value={text} onChange={e => setText(e.target.value)} placeholder="输入要发送的文字或链接（Ctrl+Enter 发送）"
        onKeyDown={e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) send(); }} />
      <button className={primaryButton} onClick={send} disabled={!text.trim()}>发送</button>
    </div>
    {msg ? <p className="text-xs text-muted">{msg}</p> : null}
    <ul className="max-h-[60vh] space-y-1 overflow-y-auto">
      {items.map(it => <li key={it.id} className="flex items-start gap-2 rounded border border-line bg-background px-2 py-1">
        <p className="min-w-0 flex-1 whitespace-pre-wrap break-all text-sm">{/^https?:\/\/\S+$/.test(it.content.trim())
          ? <a className="text-primary underline" href={it.content.trim()} target="_blank" rel="noopener noreferrer">{it.content}</a> : it.content}</p>
        <span className="shrink-0 text-xs text-muted">{new Date(it.created_at).toLocaleTimeString()}</span>
        <button className="shrink-0 text-xs text-primary hover:underline" onClick={() => copy(it.content)}>复制</button>
      </li>)}
      {!items.length ? <li className="text-sm text-muted">剪贴板是空的</li> : null}
    </ul>
  </div>;
}

const chunkSize = 4 << 20;

function SharedFiles() {
  const [files, setFiles] = useState<Shared[]>([]);
  const [msg, setMsg] = useState('');
  const [progress, setProgress] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const load = () => { api<Shared[]>('/api/tools/shared/files').then(setFiles).catch((e: Error) => setMsg(e.message)); };
  useEffect(load, []);
  const upload = async (list: FileList | null) => {
    if (!list?.length) return;
    setMsg('');
    for (const file of list) {
      const total = Math.max(1, Math.ceil(file.size / chunkSize));
      try {
        for (let i = 0; i < total; i++) {
          setProgress(`${file.name}：${Math.round(i / total * 100)}%`);
          const q = new URLSearchParams({ filename: file.name, chunk_index: String(i), ...(i === total - 1 ? { last: '1' } : {}) });
          await request(`/api/tools/shared/upload?${q}`, { method: 'POST', body: file.slice(i * chunkSize, (i + 1) * chunkSize) });
        }
        setMsg(`已上传 ${file.name}`);
      } catch (e) { setMsg(`${file.name} 上传失败：${(e as Error).message}`); break; }
    }
    setProgress(undefined);
    if (input.current) input.current.value = '';
    load();
  };
  const regular = files.filter(f => !f.is_dir);
  return <div className={card}
    onDragOver={e => e.preventDefault()} onDrop={e => { e.preventDefault(); void upload(e.dataTransfer.files); }}>
    <h3 className="font-semibold">📁 共享文件 <span className="text-xs font-normal text-muted">资源库 media_library/shared，可拖文件到这里</span></h3>
    <div className="flex flex-wrap items-center gap-2">
      <input ref={input} type="file" multiple className="hidden" onChange={e => void upload(e.target.files)} />
      <button className={primaryButton} disabled={!!progress} onClick={() => input.current?.click()}>上传文件</button>
      <a className={`${button} ${regular.length ? '' : 'pointer-events-none opacity-50'}`} href="/api/tools/shared/zip">打包下载全部</a>
      <button className={button} onClick={load}>刷新</button>
      {progress ? <span className="text-sm text-muted">{progress}</span> : msg ? <span className="text-sm text-muted">{msg}</span> : null}
    </div>
    <ul className="max-h-[60vh] space-y-1 overflow-y-auto">
      {regular.map(f => <li key={f.name} className="flex items-center gap-2 rounded border border-line bg-background px-2 py-1 text-sm">
        <a className="min-w-0 flex-1 truncate text-primary hover:underline" href={`/api/tools/shared/file?name=${encodeURIComponent(f.name)}`} download={f.name}>{f.name}</a>
        <span className="shrink-0 text-xs text-muted">{size(f.size_bytes)} · {f.mod_time.slice(5, 16)}</span>
      </li>)}
      {!regular.length ? <li className="text-sm text-muted">还没有共享文件</li> : null}
    </ul>
    <p className="text-xs text-muted">挂成网络盘（WebDAV，局域网内）：<code className="select-all">{`${window.location.protocol}//${window.location.host}/dav/`}</code>，
      用 omniHub 账号登录。Windows「映射网络驱动器」、macOS 访达「连接服务器」、Linux 文件管理器把 http:// 换成 dav:// 都能打开；删除的文件进 Deck 的回收站。</p>
  </div>;
}

function MetricsCard() {
  const [m, setM] = useState<Metrics>();
  const [err, setErr] = useState('');
  usePoll(() => { api<Metrics>('/api/tools/metrics').then(v => { setM(v); setErr(''); }).catch((e: Error) => setErr(e.message)); }, 3000);
  const bar = (used: number, total: number) => <span className="block h-1.5 overflow-hidden rounded bg-line"><span className="block h-full bg-[#58a6ff]" style={{ width: `${total ? used / total * 100 : 0}%` }} /></span>;
  return <div className={card}>
    <h3 className="font-semibold">📊 主机状态</h3>
    {err ? <p className="text-sm text-danger">{err}</p> : null}
    {m ? <div className="grid gap-3 sm:grid-cols-2">
      <div className="space-y-1 text-sm"><p>内存 <span className="text-muted">可用 {size(m.mem_avail_bytes)} / {size(m.mem_total_bytes)}</span></p>{bar(m.mem_total_bytes - m.mem_avail_bytes, m.mem_total_bytes)}</div>
      <div className="space-y-1 text-sm"><p>资源库磁盘 <span className="text-muted">可用 {size(m.disk_free_bytes)} / {size(m.disk_total_bytes)}</span></p>{bar(m.disk_total_bytes - m.disk_free_bytes, m.disk_total_bytes)}</div>
      <p className="text-xs text-muted">{m.os} · {m.num_cpu} 核</p>
    </div> : <p className="text-sm text-muted">读取中…</p>}
  </div>;
}

function Network() {
  const [speed, setSpeed] = useState('');
  const [devices, setDevices] = useState<Device[]>();
  const [mac, setMac] = useState('');
  const [msg, setMsg] = useState('');
  const [busy, setBusy] = useState(false);
  const test = async () => {
    setBusy(true); setSpeed('测速中…');
    try {
      let t = performance.now();
      const down = await (await request(`/api/tools/speed?t=${Date.now()}`)).arrayBuffer();
      const downMbps = down.byteLength * 8 / ((performance.now() - t) / 1000) / 1e6;
      const payload = new Uint8Array(4 << 20);
      t = performance.now();
      await request('/api/tools/speed', { method: 'POST', body: payload });
      const upMbps = payload.byteLength * 8 / ((performance.now() - t) / 1000) / 1e6;
      setSpeed(`下载 ${downMbps.toFixed(0)} Mbps · 上传 ${upMbps.toFixed(0)} Mbps`);
    } catch (e) { setSpeed(`测速失败：${(e as Error).message}`); }
    setBusy(false);
  };
  const scan = () => { setDevices(undefined); setMsg('正在扫描…'); api<Device[]>('/api/tools/lanscan').then(d => { setDevices(d); setMsg(''); }).catch((e: Error) => setMsg(e.message)); };
  const wake = () => { post('/api/tools/wol', { mac: mac.trim() }).then(() => setMsg(`已向 ${mac.trim()} 发送唤醒包`)).catch((e: Error) => setMsg(`唤醒失败：${e.message}`)); };
  return <div className={card}>
    <h3 className="font-semibold">🌐 网络</h3>
    <div className="flex flex-wrap items-center gap-2">
      <button className={button} disabled={busy} onClick={() => void test()}>测速（本机 ↔ 服务）</button>
      <span className="text-sm text-muted">{speed}</span>
    </div>
    <div className="flex flex-wrap items-center gap-2">
      <input className={`${select} min-w-0 flex-1 font-mono`} placeholder="要唤醒的电脑 MAC，如 AA:BB:CC:DD:EE:FF" value={mac} onChange={e => setMac(e.target.value)} />
      <button className={button} disabled={!/^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/i.test(mac.trim())} onClick={wake}>网络唤醒</button>
      <button className={button} onClick={scan}>扫描局域网</button>
    </div>
    {msg ? <p className="text-sm text-muted">{msg}</p> : null}
    {devices ? <ul className="max-h-56 space-y-1 overflow-y-auto">
      {devices.map(d => <li key={d.mac + d.ip} className="flex items-center gap-2 rounded border border-line bg-background px-2 py-1 text-sm">
        <span className="w-28 shrink-0 font-mono">{d.ip}</span>
        <span className="min-w-0 flex-1 truncate">{d.hostname || '—'}</span>
        <button className="shrink-0 font-mono text-xs text-primary hover:underline" title="填入唤醒" onClick={() => setMac(d.mac)}>{d.mac}</button>
      </li>)}
      {!devices.length ? <li className="text-sm text-muted">没有发现设备</li> : null}
    </ul> : null}
  </div>;
}
