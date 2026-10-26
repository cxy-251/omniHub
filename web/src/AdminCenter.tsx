import { lazy, Suspense, useState } from 'react';

const AdminPage = lazy(() => import('./AdminPage'));
const LibraryManager = lazy(() => import('./LibraryManager'));
const ShortVideoAdmin = lazy(() => import('./ShortVideoAdmin'));
const NetworkAdmin = lazy(() => import('./NetworkAdmin'));
const GalleryAdmin = lazy(() => import('./GalleryAdmin'));

const tabs = [['accounts', '用户与权限'], ['libraries', '资源库'], ['shortvideo', '短视频'], ['gallery', '相册'], ['network', '网络']] as const;
type Tab = typeof tabs[number][0];

// AdminCenter 是管理员后台入口，各标签页按需加载。
export default function AdminCenter() {
  const [tab, setTab] = useState<Tab>('accounts');
  return <div className="space-y-5">
    <nav className="flex gap-1 overflow-x-auto border-b border-line pb-3 sm:gap-2" aria-label="管理">
      {tabs.map(([id, label]) => <button key={id} className={`shrink-0 rounded-md px-3 py-2 text-sm sm:px-4 ${tab === id ? 'bg-panel text-primary' : 'text-muted hover:text-foreground'}`} onClick={() => setTab(id)}>{label}</button>)}
    </nav>
    <Suspense fallback={<p role="status">正在加载…</p>}>
      {tab === 'accounts' ? <AdminPage /> : null}
      {tab === 'libraries' ? <LibraryManager onChanged={() => window.dispatchEvent(new Event('omni-library-changed'))} /> : null}
      {tab === 'shortvideo' ? <ShortVideoAdmin /> : null}
      {tab === 'gallery' ? <GalleryAdmin /> : null}
      {tab === 'network' ? <NetworkAdmin /> : null}
    </Suspense>
  </div>;
}
