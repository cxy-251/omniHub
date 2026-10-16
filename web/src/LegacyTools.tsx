import { useEffect, useState } from 'react';

// 过渡：局域网工具仍是旧的原生 JS 模块（web/js/tools.js），重写为 React 前在这里挂载。
type ExistingModule = { init(): void; activate?(): void; dispose(): void };

export default function LegacyTools() {
  const [error, setError] = useState('');
  useEffect(() => {
    let disposed = false;
    let module: ExistingModule | undefined;
    (async () => { await import('../css/tools.css'); return import('../js/tools.js'); })().then(imported => {
      if (disposed) return;
      module = imported.default as unknown as ExistingModule;
      module.init();
      module.activate?.();
    }).catch((e: Error) => { if (!disposed) setError(e.message); });
    return () => { disposed = true; module?.dispose(); };
  }, []);
  return <section className="tab-pane active" id="pane-tools">
    {error ? <p role="alert" className="mb-4 text-danger">{error}</p> : null}
    <div id="tools-container" />
  </section>;
}
