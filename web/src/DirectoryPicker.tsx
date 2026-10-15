import { useEffect, useState } from 'react';
import { api } from './api';

type Entry = { name: string; path: string; is_library: boolean };
type Listing = { path: string; parent: string; is_library?: boolean; truncated?: boolean; entries: Entry[] };

const button = 'rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary disabled:opacity-50';

// DirectoryPicker 逐层浏览服务端目录（管理员接口），选中后回传绝对路径。
export default function DirectoryPicker({ onPick, onCancel }: { onPick: (path: string) => void; onCancel: () => void }) {
  const [path, setPath] = useState('');
  const [listing, setListing] = useState<Listing>();
  const [error, setError] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    setError('');
    api<Listing>(`/api/library/browse?path=${encodeURIComponent(path)}`, { signal: controller.signal })
      .then(setListing)
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [path]);

  return <div className="space-y-3 rounded-lg border border-primary bg-panel p-4">
    <div className="flex flex-wrap items-center gap-2">
      <button className={button} onClick={() => setPath('')}>起点</button>
      <button className={button} disabled={!listing?.path} onClick={() => setPath(listing?.parent ?? '')}>上一级</button>
      <span className="min-w-0 flex-1 break-all text-sm text-muted">{listing?.path || '选择起点'}</span>
    </div>
    {error ? <p role="alert" className="text-danger">{error}</p> : null}
    <ul className="max-h-80 overflow-y-auto rounded-md border border-line">
      {listing?.entries.map(entry => <li key={entry.path}>
        <button className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left hover:bg-background" onClick={() => setPath(entry.path)}>
          <span className="break-all">📁 {entry.name}</span>
          {entry.is_library ? <span className="shrink-0 text-xs text-primary">已有资源库标识</span> : null}
        </button>
      </li>)}
      {listing && !listing.entries.length ? <li className="px-3 py-2 text-sm text-muted">没有子目录</li> : null}
    </ul>
    {listing?.truncated ? <p className="text-sm text-muted">目录过多，只显示前 5000 项。</p> : null}
    <div className="flex flex-wrap gap-2">
      <button className={button} disabled={!listing?.path} onClick={() => listing && onPick(listing.path)}>选择此目录{listing?.is_library ? '（已有资源库）' : ''}</button>
      <button className={button} onClick={onCancel}>取消</button>
    </div>
  </div>;
}
