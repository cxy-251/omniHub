import { useEffect, useState } from 'react';
import type { ModuleID } from './App';

type ExistingModule = {
  init(): void;
  activate?(): void;
  dispose(): void;
  switchSubtab?(id: string): void;
};

const loaders = {
  media: async () => { await import('../css/media.css'); return import('../js/media.js'); },
  games: async () => { await import('../css/games.css'); return import('../js/games.js'); },
  tools: async () => { await import('../css/tools.css'); return import('../js/tools.js'); },
  tasks: async () => { await import('../css/tasks.css'); return import('../js/tasks.js'); },
  admin: async () => { await import('../css/admin.css'); return import('../js/admin.js'); },
};

// Transitional lifecycle adapter: existing modules remain runnable while each is moved to typed components.
export default function ModulePage({ id, sections }: { id: ModuleID; sections: string[] }) {
  const [error, setError] = useState('');
  useEffect(() => {
    let disposed = false;
    let module: ExistingModule | undefined;
    loaders[id]().then(imported => {
      if (disposed) return;
      module = imported.default as unknown as ExistingModule;
      module.init();
      if (id === 'media') {
        document.querySelectorAll<HTMLButtonElement>('.media-subnav [data-sub]').forEach(button => {
          if (!sections.includes(button.dataset.sub!)) button.remove();
        });
        const first = sections.find(s => ['gallery', 'shortvideo', 'text', 'manga'].includes(s));
        if (first) module.switchSubtab?.(first);
      } else module.activate?.();
    }).catch((e: Error) => { if (!disposed) setError(e.message); });
    return () => { disposed = true; module?.dispose(); };
  }, [id, sections]);
  return <section className="tab-pane active" id={`pane-${id}`}>
    {error ? <p role="alert" className="mb-4 whitespace-pre-wrap text-danger">{error}</p> : null}
    <div id={`${id}-container`} />
  </section>;
}
