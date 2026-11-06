import { Suspense, useSyncExternalStore } from 'react';
import { lazyPage, PageBoundary, PageLoading } from './PageBoundary';

const AdminPage = lazyPage(() => import('./AdminPage'));
const SessionSettings = lazyPage(() => import('./SessionSettings'));
const LibraryManager = lazyPage(() => import('./LibraryManager'));
const ShortVideoAdmin = lazyPage(() => import('./ShortVideoAdmin'));
const NetworkAdmin = lazyPage(() => import('./NetworkAdmin'));
const GalleryAdmin = lazyPage(() => import('./GalleryAdmin'));
const PerfAdmin = lazyPage(() => import('./PerfAdmin'));

const tabs = [['accounts', '用户与权限'], ['libraries', '资源库'], ['shortvideo', '短视频'], ['gallery', '相册'], ['network', '网络'], ['perf', '性能']] as const;
type Tab = typeof tabs[number][0];
const accountTabs = [['session', '登录与会话'], ['permissions', '访问权限'], ['users', '用户管理'], ['devices', '信任设备']] as const;
const subscribe = (fn: () => void) => { window.addEventListener('hashchange', fn); return () => window.removeEventListener('hashchange', fn); };
const snapshot = () => window.location.hash;

// AdminCenter 是管理员后台入口，各标签页按需加载。
export default function AdminCenter() {
  const hash = useSyncExternalStore(subscribe, snapshot);
  const [, section, subpage] = hash.split('/');
  const tab: Tab = tabs.find(([id]) => id === section)?.[0] ?? 'accounts';
  const account = accountTabs.find(([id]) => id === subpage)?.[0] ?? 'session';
  return <div className="settings-center">
    <nav className="settings-tabs" aria-label="管理">
      {tabs.map(([id, label]) => <a key={id} href={`#admin/${id}`} aria-current={tab === id ? 'page' : undefined}>{label}</a>)}
    </nav>
    {tab === 'accounts' ? <nav className="settings-subtabs" aria-label="账号设置">{accountTabs.map(([id, label]) => <a key={id} href={`#admin/accounts/${id}`} aria-current={account === id ? 'page' : undefined}>{label}</a>)}</nav> : null}
    <PageBoundary key={`${tab}/${account}`} name="设置"><Suspense fallback={<PageLoading />}>
      {tab === 'accounts' ? account === 'session' ? <SessionSettings /> : <AdminPage key={account} view={account} /> : null}
      {tab === 'libraries' ? <LibraryManager onChanged={() => window.dispatchEvent(new Event('omni-library-changed'))} /> : null}
      {tab === 'shortvideo' ? <ShortVideoAdmin /> : null}
      {tab === 'gallery' ? <GalleryAdmin /> : null}
      {tab === 'network' ? <NetworkAdmin /> : null}
      {tab === 'perf' ? <PerfAdmin /> : null}
    </Suspense></PageBoundary>
  </div>;
}
