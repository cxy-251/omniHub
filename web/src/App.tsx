import { lazy, Suspense, useEffect, useState, type FormEvent } from 'react';
import { api, post, type Status } from './api';

const PasswordChange = lazy(() => import('./PasswordChange'));
const ModulePage = lazy(() => import('./ModulePage'));
const LibraryManager = lazy(() => import('./LibraryManager'));
const modules = [
  { id: 'media', title: '媒体专区', description: '相册、漫画、文本、音频与短视频' },
  { id: 'games', title: '网页游戏', description: 'RPG、模拟器、SLG 与 Flash' },
  { id: 'tools', title: '局域网工具', description: '文件共享、剪贴板与只读状态看板' },
  { id: 'tasks', title: '任务调试台', description: '脚本任务与实时日志' },
  { id: 'admin', title: '权限管理', description: '用户与分类访问权限' },
  { id: 'libraries', title: '资源库管理', description: '挂载、离线恢复与主库状态迁移' },
] as const;
export type ModuleID = typeof modules[number]['id'];
type Libraries = { primary: { name: string } | null };

export default function App() {
  const [status, setStatus] = useState<Status>();
  const [sections, setSections] = useState<string[]>([]);
  const [library, setLibrary] = useState<string>('加载资源库…');
  const [active, setActive] = useState<ModuleID | 'portal'>('portal');
  const [error, setError] = useState('');
  const [loginOpen, setLoginOpen] = useState(false);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [libraryRevision, setLibraryRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      api<Status>('/api/auth/status', { signal: controller.signal }),
      api<{ sections: string[] }>('/api/sections', { signal: controller.signal }),
      api<Libraries>('/api/library/list', { signal: controller.signal }),
    ]).then(([nextStatus, access, libraries]) => {
      setStatus(nextStatus); setSections(access.sections);
      setLibrary(libraries.primary ? `主库：${libraries.primary.name}` : '尚未配置资源库');
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [libraryRevision]);

  const visible = modules.filter(m => m.id === 'media'
    ? sections.some(s => ['gallery', 'shortvideo', 'text', 'audio', 'manga'].includes(s))
    : sections.includes(m.id));

  async function login(event: FormEvent) {
    event.preventDefault(); setPending(true); setError('');
    try {
      await post('/api/auth/login', { username, password });
      const [next, access] = await Promise.all([api<Status>('/api/auth/status'), api<{ sections: string[] }>('/api/sections')]);
      setStatus(next); setSections(access.sections); setPassword(''); setLoginOpen(false); setActive('portal');
    } catch (e) { setError((e as Error).message); } finally { setPending(false); }
  }

  const navigationStyle = (selected: boolean) => `shrink-0 rounded-md px-4 py-2 text-sm ${selected ? 'bg-panel text-primary' : 'text-muted hover:bg-panel hover:text-foreground'}`;
  return <div className="min-h-screen bg-background text-foreground antialiased">
    <header className="sticky top-0 z-20 flex h-auto min-h-14 flex-wrap items-center justify-between gap-3 border-b border-line bg-panel px-4 py-3 sm:px-6">
      <div className="flex flex-wrap items-center gap-3"><h1 className="text-xl font-semibold text-primary">omniHub</h1><span className="text-sm text-muted">{library}</span></div>
      <div className="flex items-center gap-3"><span className="text-sm text-muted">{status?.user.username ?? '访客'}{status ? ` · Lv.${status.user.level}` : ''}</span>
        <button className="rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary" onClick={() => setLoginOpen(!loginOpen)}>登录账号</button>
 {status?.logged_in ? <button className="rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary" onClick={() => setPasswordOpen(value => !value)}>修改密码</button> : null}
 {status?.logged_in ? <button className="rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary" onClick={() => { void post('/api/auth/logout', {}).then(() => window.location.reload()).catch((e: Error) => setError(e.message)); }}>退出登录</button> : null}</div>
    </header>
    <nav className="flex gap-2 overflow-x-auto border-b border-line px-4 py-3 sm:px-6" aria-label="主要导航">
      <button className={navigationStyle(active === 'portal')} onClick={() => setActive('portal')}>门户大厅</button>
      {visible.map(m => <button key={m.id} className={navigationStyle(active === m.id)} onClick={() => setActive(m.id)}>{m.title}</button>)}
    </nav>
    <main className="mx-auto max-w-screen-2xl p-4 sm:p-6">
      {error ? <p role="alert" className="mb-4 whitespace-pre-wrap text-danger">{error}</p> : null}
      {passwordOpen && status?.logged_in ? <Suspense fallback={<p role="status">正在加载…</p>}><PasswordChange onCancel={() => setPasswordOpen(false)} /></Suspense> : null}
      {loginOpen ? <form className="mb-6 flex flex-wrap items-end gap-3 rounded-lg border border-line bg-panel p-4" onSubmit={login}>
        <label className="flex flex-col gap-2 text-sm">用户名<input className="min-w-40 rounded-md border border-line bg-background px-3 py-2" autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required maxLength={64} /></label>
        <label className="flex flex-col gap-2 text-sm">密码<input className="min-w-40 rounded-md border border-line bg-background px-3 py-2" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required maxLength={72} /></label>
        <button className="rounded-md border border-primary px-4 py-2 text-primary disabled:opacity-50" disabled={pending}>{pending ? '登录中…' : '登录'}</button>
      </form> : null}
      {active === 'portal' ? <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {visible.map(m => <button key={m.id} className="rounded-lg border border-line bg-panel p-5 text-left transition-colors hover:border-primary focus-visible:outline-2 focus-visible:outline-primary" onClick={() => setActive(m.id)}><h3 className="mb-2 text-lg font-semibold">{m.title}</h3><p className="text-sm text-muted">{m.description}</p></button>)}
        {!status && !error ? <p role="status">正在连接服务…</p> : null}
      </section> : <Suspense fallback={<p role="status">正在加载模块…</p>}>
        {active === 'libraries' ? <LibraryManager onChanged={name => { setLibrary(name ? `主库：${name}` : '尚未配置主库'); setLibraryRevision(value => value + 1); }} /> : <ModulePage key={`${active}-${status?.user.id}`} id={active} sections={sections} />}
      </Suspense>}
    </main>
  </div>;
}
