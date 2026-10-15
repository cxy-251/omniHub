import { lazy, Suspense, useState } from 'react';

const AdminPage = lazy(() => import('./AdminPage'));
const LibraryManager = lazy(() => import('./LibraryManager'));
const ShortVideoAdmin = lazy(() => import('./ShortVideoAdmin'));

const tabs = [['accounts', '用户与权限'], ['libraries', '资源库'], ['shortvideo', '短视频']] as const;
type Tab = typeof tabs[number][0];

// AdminCenter 是管理员后台入口，各标签页按需加载。
export default function AdminCenter() {
  const [tab, setTab] = useState<Tab>('accounts');
  return <div className="space-y-5">
    <nav className="flex gap-2 border-b border-line pb-3" aria-label="管理">
      {tabs.map(([id, label]) => <button key={id} className={`rounded-md px-4 py-2 text-sm ${tab === id ? 'bg-panel text-primary' : 'text-muted hover:text-foreground'}`} onClick={() => setTab(id)}>{label}</button>)}
    </nav>
    <Suspense fallback={<p role="status">正在加载…</p>}>
      {tab === 'accounts' ? <AdminPage /> : null}
      {tab === 'libraries' ? <LibraryManager onChanged={() => window.dispatchEvent(new Event('omni-library-changed'))} /> : null}
      {tab === 'shortvideo' ? <ShortVideoAdmin /> : null}
    </Suspense>
  </div>;
}
