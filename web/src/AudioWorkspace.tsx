import { Suspense } from 'react';
import { lazyPage, PageBoundary, PageLoading } from './PageBoundary';
import { useHash } from './useHash';
import './music.css';

const Library = lazyPage(() => import('./AudioPage'));
const Albums = lazyPage(() => import('./MusicAlbums'));
const Listening = lazyPage(() => import('./ListeningPage'));
const views = [['library', '音乐库'], ['albums', '专辑'], ['listen', '正在收听']] as const;

/** Web audio views share the global player but mount only the active workspace. */
export default function AudioWorkspace() {
  const route = useHash().split('?')[0].split('/')[1];
  const current = views.find(([id]) => id === route)?.[0] ?? 'library';
  return <section className="audio-workspace">
    <nav className="settings-tabs" aria-label="音声页面">{views.map(([id, name]) => <a key={id} href={`#audio/${id}`} aria-current={current === id ? 'page' : undefined}>{name}</a>)}</nav>
    <PageBoundary key={current} name="音声"><Suspense fallback={<PageLoading />}>
      {current === 'library' ? <Library active /> : current === 'albums' ? <Albums /> : <Listening />}
    </Suspense></PageBoundary>
  </section>;
}
