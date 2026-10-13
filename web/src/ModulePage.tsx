import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import type { ModuleID } from './App';

type ExistingModule = {
  init(): void;
  activate?(): void;
  dispose(): void;
  switchSubtab?(id: string): void;
  playAudio?(path: string, title: string, album: string): void;
};

const loaders = {
  media: async () => { await import('../css/media.css'); return import('../js/media.js'); },
  games: async () => { await import('../css/games.css'); return import('../js/games.js'); },
  tools: async () => { await import('../css/tools.css'); return import('../js/tools.js'); },
  tasks: async () => { await import('../css/tasks.css'); return import('../js/tasks.js'); },
  admin: async () => { await import('../css/admin.css'); return import('../js/admin.js'); },
};

const AudioLibrary = lazy(() => import('./AudioLibrary'));
const GalleryLibrary = lazy(() => import('./GalleryLibrary'));

// Transitional lifecycle adapter: existing modules remain runnable while each is moved to typed components.
export default function ModulePage({ id, sections }: { id: Exclude<ModuleID, 'libraries'>; sections: string[] }) {
  const [error, setError] = useState('');
  const [audioActive, setAudioActive] = useState(false);
  const [audioVisited, setAudioVisited] = useState(false);
  const [galleryActive, setGalleryActive] = useState(false);
  const [galleryVisited, setGalleryVisited] = useState(false);
  const moduleRef = useRef<ExistingModule | undefined>(undefined);
  useEffect(() => {
    let disposed = false;
    let module: ExistingModule | undefined;
    const tabChanged = (event: Event) => {
      const tab = (event as CustomEvent<string>).detail;
      const gallery = sections.includes('gallery') && tab === 'gallery';
      setGalleryActive(gallery);
      if (gallery) setGalleryVisited(true);
      const active = sections.includes('audio') && tab === 'audio';
      setAudioActive(active);
      if (active) setAudioVisited(true);
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
  return <section className="tab-pane active" id={`pane-${id}`}>
    {error ? <p role="alert" className="mb-4 whitespace-pre-wrap text-danger">{error}</p> : null}
    <div id={`${id}-container`} />
    {id === 'media' && sections.includes('gallery') && galleryVisited ? <div hidden={!galleryActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载相册…</p>}><GalleryLibrary active={galleryActive} canEdit={sections.includes('admin')} /></Suspense>
    </div> : null}
    {id === 'media' && sections.includes('audio') && audioVisited ? <div hidden={!audioActive} className="mt-4">
      <Suspense fallback={<p role="status">正在加载音频专辑…</p>}><AudioLibrary active={audioActive} onPlay={(path, title, album) => moduleRef.current?.playAudio?.(path, title, album)} /></Suspense>
    </div> : null}
  </section>;
}
