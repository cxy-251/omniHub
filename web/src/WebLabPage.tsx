import { Suspense, useEffect, useState } from 'react';
import { lazyPage, PageBoundary, PageLoading } from './PageBoundary';

const RubiksLab = lazyPage(() => import('./weblab/RubiksLab'));
const FluidLab = lazyPage(() => import('./weblab/FluidLab'));
const FourierPendulumLab = lazyPage(() => import('./weblab/FourierPendulumLab'));
const SlideDeckLab = lazyPage(() => import('./weblab/SlideDeckLab'));

const ALL_SUBTABS = [
  ['rubiks', '3D 魔方'],
  ['fluid', 'WebGL 流体'],
  ['fourier', '傅里叶与双摆'],
  ['slides', '网页幻灯片'],
] as const;

type SubTabId = typeof ALL_SUBTABS[number][0];

export default function WebLabPage({ sections }: { sections: string[] }) {
  const visibleTabs = ALL_SUBTABS.filter(([id]) => sections.includes(`weblab/${id}`));

  const fromHash = (): SubTabId | undefined => {
    const sub = window.location.hash.split('/')[1];
    return ALL_SUBTABS.find(([id]) => id === sub)?.[0];
  };

  const [tab, setTab] = useState<SubTabId>(() => fromHash() ?? 'rubiks');

  useEffect(() => {
    const sync = () => {
      const next = fromHash();
      if (next) setTab(next);
    };
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);

  const current: SubTabId | undefined = visibleTabs.some(([id]) => id === tab)
    ? tab
    : visibleTabs[0]?.[0];

  if (!current) {
    return <p className="text-sm text-muted">当前账号没有可访问的实验室子模块。</p>;
  }

  return (
    <div className="space-y-4">
      <nav className="settings-tabs" aria-label="实验室子模块">
        {visibleTabs.map(([id, label]) => (
          <a key={id} href={`#weblab/${id}`} aria-current={current === id ? 'page' : undefined} onClick={() => setTab(id)}>
            {label}
          </a>
        ))}
      </nav>

      <PageBoundary key={current} name={ALL_SUBTABS.find(([id]) => id === current)?.[1]}>
        <Suspense fallback={<PageLoading />}>
          {current === 'rubiks' ? <RubiksLab /> : null}
          {current === 'fluid' ? <FluidLab /> : null}
          {current === 'fourier' ? <FourierPendulumLab /> : null}
          {current === 'slides' ? <SlideDeckLab /> : null}
        </Suspense>
      </PageBoundary>
    </div>
  );
}
