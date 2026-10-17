import { lazy, Suspense, useEffect, useState, type FormEvent } from 'react';
import { api, post, type Status } from './api';
import { AudioBar, AudioProvider } from './AudioEngine';
import { store, stored } from './ui';

const PasswordChange = lazy(() => import('./PasswordChange'));
const ShortVideoPage = lazy(() => import('./ShortVideoPage'));
const GalleryPage = lazy(() => import('./GalleryPage'));
const AudioPage = lazy(() => import('./AudioPage'));
const TextPage = lazy(() => import('./TextPage'));
const MangaLibrary = lazy(() => import('./MangaLibrary'));
const GameLibrary = lazy(() => import('./GameLibrary'));
const ToolsPage = lazy(() => import('./ToolsPage'));
const AdminCenter = lazy(() => import('./AdminCenter'));

// 导航与桌面端一致；只显示当前账号有权限的分区（服务端 /api/sections 决定）。
const tabs = [
  ['shortvideo', '短视频'], ['gallery', '相册'], ['audio', '音声'], ['text', '文本'], ['manga', '漫画'],
  ['games', '游戏'], ['tools', '工具'], ['admin', '设置'],
] as const;
type Tab = typeof tabs[number][0];

const GamePlayer = lazy(() => import('./GameLibrary').then(m => ({ default: m.GamePlayer })));

export default function App() {
  // #play/<库ID>/<游戏ID>：新标签页里只放游戏本身（游戏页点卡片时打开）。
  if (window.location.hash.startsWith('#play/')) {
    const [lib, id] = window.location.hash.slice(6).split('/').map(decodeURIComponent);
    return <Suspense fallback={null}><GamePlayer libraryId={lib} id={id} /></Suspense>;
  }
  return <AudioProvider><Shell /></AudioProvider>;
}
// 同一页里地址改成 #play/…（比如手动粘贴）时重新加载，进入游戏页。
window.addEventListener('hashchange', () => { if (window.location.hash.startsWith('#play/')) window.location.reload(); });

function Shell() {
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
      setStatus(nextStatus); setSections(access.sections);
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

  const tabStyle = (selected: boolean) => `shrink-0 rounded-md px-3 py-1 text-sm ${selected ? 'bg-panel text-primary' : 'text-muted hover:bg-panel hover:text-foreground'}`;
  const headerButton = 'rounded-md border border-line px-2.5 py-1 text-sm hover:border-primary';
  return <div className="min-h-screen bg-background pb-16 text-foreground antialiased">
    <header className="sticky top-0 z-30 border-b border-line bg-background">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-1">
        <h1 className="text-lg font-semibold text-primary">omniHub</h1>
        <nav className="order-last flex min-w-0 basis-full gap-1 overflow-x-auto sm:order-none sm:flex-1 sm:basis-auto" aria-label="主要导航">
          {visible.map(([id, label]) => <button key={id} className={tabStyle(current === id)} onClick={() => choose(id)}>{label}</button>)}
        </nav>
        <span className="ml-auto text-sm text-muted sm:ml-0">{status?.user.username ?? '访客'}{status ? ` · Lv.${status.user.level}` : ''}</span>
        {status?.logged_in
          ? <><button className={headerButton} onClick={() => setPasswordOpen(v => !v)}>修改密码</button>
            <button className={headerButton} onClick={() => { void post('/api/auth/logout', {}).then(() => window.location.reload()).catch((e: Error) => setError(e.message)); }}>退出</button></>
          : <button className={headerButton} onClick={() => setLoginOpen(v => !v)}>登录</button>}
      </div>
    </header>
    <main className="mx-auto max-w-screen-2xl px-3 py-2">
      {error ? <p role="alert" className="mb-3 whitespace-pre-wrap text-danger">{error}</p> : null}
      {passwordOpen && status?.logged_in ? <Suspense fallback={null}><PasswordChange onCancel={() => setPasswordOpen(false)} /></Suspense> : null}
      {loginOpen ? <form className="mb-4 flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4" onSubmit={login}>
        <label className="flex flex-col gap-1 text-sm">用户名<input className="min-w-40 rounded-md border border-line bg-background px-3 py-1.5" autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required maxLength={64} /></label>
        <label className="flex flex-col gap-1 text-sm">密码<input className="min-w-40 rounded-md border border-line bg-background px-3 py-1.5" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required maxLength={72} /></label>
        <button className="rounded-md border border-primary px-4 py-1.5 text-primary disabled:opacity-50" disabled={pending}>{pending ? '登录中…' : '登录'}</button>
      </form> : null}
      {!status && !error ? <p role="status">正在连接服务…</p> : null}
      {status && !visible.length ? <p className="text-muted">当前账号没有可访问的内容，请登录。</p> : null}
      <Suspense fallback={<p role="status">正在加载…</p>}>
        {current === 'shortvideo' ? <ShortVideoPage active /> : null}
        {current === 'gallery' ? <GalleryPage active isAdmin={status?.user.role === 'admin'} /> : null}
        {current === 'audio' ? <AudioPage active /> : null}
        {current === 'text' ? <TextPage active /> : null}
        {current === 'manga' ? <MangaLibrary active /> : null}
        {current === 'games' ? <GameLibrary /> : null}
        {current === 'tools' ? <ToolsPage sections={sections} /> : null}
        {current === 'admin' ? <AdminCenter /> : null}
      </Suspense>
    </main>
    <Toast />
    <AudioBar onOpen={() => choose('audio')} />
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
