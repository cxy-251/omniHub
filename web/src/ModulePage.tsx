import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { ModuleID } from './App';

type ExistingModule = {
  init(): void;
  activate?(): void;
  dispose(): void;
  switchSubtab?(id: string): void;
  playAudio?(path: string, title: string, album: string): void;
  updateAudioLabels?(path: string, title: string, album: string): void;
  showAudioBar?(): void;
};

const loaders = {
  media: async () => { await import('../css/media.css'); return import('../js/media.js'); },
  tools: async () => { await import('../css/tools.css'); return import('../js/tools.js'); },
};

const AudioLibrary = lazy(() => import('./AudioLibrary'));
const GalleryLibrary = lazy(() => import('./GalleryLibrary'));
const VideoMatrix = lazy(() => import('./VideoMatrix'));
const TextLibrary = lazy(() => import('./TextLibrary'));
const MangaLibrary = lazy(() => import('./MangaLibrary'));
const AdminPage = lazy(() => import('./AdminPage'));
const TaskConsole = lazy(() => import('./TaskConsole'));
const GameLibrary = lazy(() => import('./GameLibrary'));

// Transitional lifecycle adapter: existing modules remain runnable while each is moved to typed components.
export default function ModulePage({ id, sections }: { id: Exclude<ModuleID, 'libraries'>; sections: string[] }) {
  const [error, setError] = useState('');
  const [mediaTab, setMediaTab] = useState('');
  const [visited, setVisited] = useState<string[]>([]);
  const moduleRef = useRef<ExistingModule | undefined>(undefined);
  useEffect(() => {
    if (id === 'games' || id === 'tasks' || id === 'admin') return;
    let disposed = false;
    let module: ExistingModule | undefined;
    const tabChanged = (event: Event) => {
      const tab = (event as CustomEvent<string>).detail;
      if (!sections.includes(tab)) return;
      setMediaTab(tab);
      setVisited(previous => previous.includes(tab) ? previous : [...previous, tab]);
    };
    if (id === 'media') window.addEventListener('omni-media-tab', tabChanged);
    loaders[id]().then(imported => {
      if (disposed) return;
      module = imported.default as unknown as ExistingModule;
      moduleRef.current = module;
      module.init();
      if (id === 'media') {
        document.querySelectorAll<HTMLButtonElement>('.media-subnav [data-sub]').forEach(button => {
          if (!sections.includes(button.dataset.sub!)) button.remove();
        });
        const first = sections.find(s => ['gallery', 'shortvideo', 'text', 'manga', 'audio'].includes(s));
        if (first) module.switchSubtab?.(first);
      } else module.activate?.();
    }).catch((e: Error) => { if (!disposed) setError(e.message); });
    return () => { disposed = true; window.removeEventListener('omni-media-tab', tabChanged); moduleRef.current = undefined; module?.dispose(); };
  }, [id, sections]);
  const audioActive = mediaTab === 'audio', audioVisited = visited.includes('audio');
  const textActive = mediaTab === 'text', textVisited = visited.includes('text');
  const videoActive = mediaTab === 'shortvideo', videoVisited = visited.includes('shortvideo');
  const galleryActive = mediaTab === 'gallery', galleryVisited = visited.includes('gallery');
  return <section className="tab-pane active" id={`pane-${id}`}>
    {error ? <p role="alert" className="mb-4 whitespace-pre-wrap text-danger">{error}</p> : null}
    <div id={`${id}-container`} />
    {id === 'admin' ? <Suspense fallback={<p role="status">正在加载管理页面…</p>}><AdminPage /></Suspense> : null}
    {id === 'tasks' ? <Suspense fallback={<p role="status">正在加载任务台…</p>}><TaskConsole /></Suspense> : null}
    {id === 'games' ? <Suspense fallback={<p role="status">正在加载游戏大厅…</p>}><GameLibrary /></Suspense> : null}
    {id === 'media' && sections.includes('manga') && visited.includes('manga') ? <div hidden={mediaTab !== 'manga'} className="mt-4">
      <Suspense fallback={<p role="status">正在加载漫画阅读器…</p>}><MangaLibrary active={mediaTab === 'manga'} /></Suspense>
    </div> : null}
    {id === 'media' && sections.includes('text') && textVisited ? <div hidden={!textActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载文本阅读器…</p>}><TextLibrary active={textActive} /></Suspense>
    </div> : null}
    {id === 'media' && sections.includes('shortvideo') && videoVisited ? <div hidden={!videoActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载视频矩阵…</p>}><VideoMatrix active={videoActive} onAccompany={sections.includes('audio') ? () => moduleRef.current?.showAudioBar?.() : undefined} /></Suspense>
    </div> : null}
    {id === 'media' && sections.includes('gallery') && galleryVisited ? <div hidden={!galleryActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载相册…</p>}><GalleryLibrary active={galleryActive} canEdit={sections.includes('admin')} /></Suspense>
    </div> : null}
    {id === 'media' && sections.includes('audio') && audioVisited ? <div hidden={!audioActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载音频专辑…</p>}><AudioLibrary active={audioActive} onPlay={(path, title, album) => moduleRef.current?.playAudio?.(path, title, album)} onMetadata={(path, title, album) => moduleRef.current?.updateAudioLabels?.(path, title, album)} /></Suspense>
    </div> : null}
  </section>;
}
