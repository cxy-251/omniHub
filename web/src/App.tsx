import { Suspense, useEffect, useState, type FormEvent } from 'react';
import { api, post, type Status } from './api';
import { AudioBar, AudioProvider } from './AudioEngine';
import { store, stored } from './ui';
import { lazyPage, PageBoundary, PageLoading } from './PageBoundary';
import { tabs, type Tab, WorkbenchNav, CompactNavigation } from './WorkbenchNav';
import MangaDockHost from './MangaDockHost';
import { PerfOverlay } from './PerfAdmin';

const PasswordChange = lazyPage(() => import('./PasswordChange'));
const ShortVideoPage = lazyPage(() => import('./ShortVideoPage'));
const GalleryPage = lazyPage(() => import('./GalleryPage'));
const AudioPage = lazyPage(() => import('./AudioWorkspace'));
const TextPage = lazyPage(() => import('./TextPage'));
const MangaLibrary = lazyPage(() => import('./MangaLibrary'));
const GameLibrary = lazyPage(() => import('./GameLibrary'));
const ToolsPage = lazyPage(() => import('./ToolsPage'));
const AdminCenter = lazyPage(() => import('./AdminCenter'));
const GamePlayer = lazyPage(() => import('./GameLibrary').then(m => ({ default: m.GamePlayer })));

export default function App() {
  // #play/<库ID>/<游戏ID>：新标签页里只放游戏本身（游戏页点卡片时打开）。
  if (window.location.hash.startsWith('#play/')) {
    const [lib, id] = window.location.hash.slice(6).split('/').map(decodeURIComponent);
    return <PageBoundary name="游戏"><Suspense fallback={<PageLoading />}><GamePlayer libraryId={lib} id={id} /></Suspense></PageBoundary>;
  }
  return <AudioProvider><Shell /></AudioProvider>;
}
// 同一页里地址改成 #play/…（比如手动粘贴）时重新加载，进入游戏页。
window.addEventListener('hashchange', () => { if (window.location.hash.startsWith('#play/')) window.location.reload(); });

function Shell() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => stored('omnihub.sidebar.collapsed', false));
  const toggleSidebar = () => {
    const next = !sidebarCollapsed;
    setSidebarCollapsed(next); store('omnihub.sidebar.collapsed', next);
    requestAnimationFrame(() => document.getElementById('sidebar-toggle')?.focus());
  };
  const [status, setStatus] = useState<Status>();
  const [sections, setSections] = useState<string[]>([]);
  // 当前标签记在网址 #后（可前进/后退、可直接打开某页），没有时用上次的。
  const fromHash = () => tabs.find(([id]) => window.location.hash.split('/')[0] === `#${id}`)?.[0];   // #gallery/matrix 直接打开多联放映
  const [tab, setTab] = useState<Tab>(() => fromHash() ?? stored('omnihub.tab', 'shortvideo'));
  useEffect(() => {
    const changed = () => { const t = fromHash(); if (t) setTab(t); };
    window.addEventListener('hashchange', changed);
    return () => window.removeEventListener('hashchange', changed);
  }, []);
  const [error, setError] = useState('');
  const [loginOpen, setLoginOpen] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      api<Status>('/api/auth/status', { signal: controller.signal }),
      api<{ sections: string[] }>('/api/sections', { signal: controller.signal }),
    ]).then(([nextStatus, access]) => {
      setStatus(nextStatus); setSections(access.sections ?? []); setError('');
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    const changed = () => setRevision(v => v + 1);
    window.addEventListener('omni-library-changed', changed);
    return () => window.removeEventListener('omni-library-changed', changed);
  }, []);
  const visible = tabs.filter(([id]) => sections.includes(id));
  // 记住的标签没有权限时，退到第一个可见标签。
  const current: Tab | undefined = visible.some(([id]) => id === tab) ? tab : visible[0]?.[0];
  const choose = (id: Tab) => { setTab(id); store('omnihub.tab', id); window.location.hash = id; };

  async function login(event: FormEvent) {
    event.preventDefault(); setPending(true); setError('');
    try {
      await post('/api/auth/login', { username, password });
      setPassword(''); setLoginOpen(false); setRevision(v => v + 1);
    } catch (e) { setError((e as Error).message); } finally { setPending(false); }
  }

  const currentLabel = tabs.find(([id]) => id === current)?.[1];
  useEffect(() => { document.title = currentLabel ? `${currentLabel} · omniHub` : 'omniHub'; }, [currentLabel]);
  const account = <details className="account-menu" onKeyDown={event => { if (event.key === 'Escape') { event.currentTarget.open = false; event.currentTarget.querySelector('summary')?.focus(); } }}>
    <summary title={status?.user.username ?? '访客'}><span aria-hidden="true">◎</span><span className="account-label">{status?.user.username ?? '访客'}</span></summary>
    <div className="account-menu-content" onClick={event => { const details = event.currentTarget.closest('details'); if (details) details.open = false; }}>
      {status?.logged_in ? <><button onClick={() => setPasswordOpen(v => !v)}>修改密码</button><button onClick={() => { void post('/api/auth/logout', {}).then(() => window.location.reload()).catch((e: Error) => setError(e.message)); }}>退出登录</button></>
        : <button onClick={() => setLoginOpen(v => !v)}>登录</button>}
    </div>
  </details>;
  const navigation = { visible, current, onChoose: choose, account };
  return <div className={`workbench${sidebarCollapsed ? ' sidebar-collapsed' : ''}`} style={{ paddingRight: 'var(--manga-dock, 0px)' }}>
    <a className="skip-navigation" href="#page-content" onClick={event => { event.preventDefault(); document.getElementById('page-content')?.focus(); }}>跳到内容</a>
    {sidebarCollapsed ? <button id="sidebar-toggle" className="sidebar-toggle sidebar-restore" aria-label="展开侧栏" title="展开侧栏" aria-expanded="false" onClick={toggleSidebar}>›</button>
      : <WorkbenchNav {...navigation} onCollapse={toggleSidebar} />}
    <div className="workbench-body">
    <main id="page-content" tabIndex={-1} className={`workbench-content ${current === 'admin' ? 'settings-content' : ''}`}>
      <CompactNavigation {...navigation} />
      {error ? <p role="alert" className="mb-3 whitespace-pre-wrap text-danger">{error}</p> : null}
      {passwordOpen && status?.logged_in ? <PageBoundary name="修改密码"><Suspense fallback={<PageLoading />}><PasswordChange onCancel={() => setPasswordOpen(false)} /></Suspense></PageBoundary> : null}
      {loginOpen ? <form className="mb-4 flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4" data-dirty={!!username || !!password || pending} onSubmit={login}>
        <label className="flex flex-col gap-1 text-sm">用户名<input className="min-w-40 rounded-md border border-line bg-background px-3 py-1.5" autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required maxLength={64} /></label>
        <label className="flex flex-col gap-1 text-sm">密码<input className="min-w-40 rounded-md border border-line bg-background px-3 py-1.5" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required maxLength={72} /></label>
        <button className="rounded-md border border-primary px-4 py-1.5 text-primary disabled:opacity-50" disabled={pending}>{pending ? '登录中…' : '登录'}</button>
      </form> : null}
      {!status && !error ? <p role="status">正在连接服务…</p> : null}
      {status && !visible.length ? <p className="text-muted">当前账号没有可访问的内容，请登录。</p> : null}
      <PageBoundary key={current} name={currentLabel}><Suspense fallback={<PageLoading />}>
        {current === 'shortvideo' ? <ShortVideoPage active isAdmin={status?.user.role === 'admin'} loggedIn={!!status?.logged_in} /> : null}
        {current === 'gallery' ? <GalleryPage active isAdmin={status?.user.role === 'admin'} loggedIn={!!status?.logged_in} /> : null}
        {current === 'audio' ? <AudioPage /> : null}
        {current === 'text' ? <TextPage active /> : null}
        {current === 'manga' ? <MangaLibrary active /> : null}
        {current === 'games' ? <GameLibrary loggedIn={!!status?.logged_in} /> : null}
        {current === 'tools' ? <ToolsPage sections={sections} isAdmin={status?.user.role === 'admin'} /> : null}
        {current === 'admin' ? <AdminCenter /> : null}
      </Suspense></PageBoundary>
    </main>
    </div>
    <MangaDockHost />
    <PerfOverlay />
    <Toast />
    <AudioBar onOpen={() => { choose('audio'); window.location.hash = 'audio/listen'; }} />
  </div>;
}

// 全局提示（ui.ts 的 notify）：底部居中，2.5 秒后消失；放在 viewer/矩阵等全屏层之上。
function Toast() {
  const [message, setMessage] = useState('');
  useEffect(() => {
    let timer = 0;
    const show = (e: Event) => { setMessage((e as CustomEvent<string>).detail); window.clearTimeout(timer); timer = window.setTimeout(() => setMessage(''), 2500); };
    window.addEventListener('omnihub-notice', show);
    return () => { window.removeEventListener('omnihub-notice', show); window.clearTimeout(timer); };
  }, []);
  return message ? <div role="status" className="fixed bottom-20 left-1/2 z-[100] -translate-x-1/2 rounded-full bg-[#30363d] px-4 py-2 text-sm text-white shadow-lg">{message}</div> : null;
}
