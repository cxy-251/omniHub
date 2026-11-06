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
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="tablist" aria-label="实验室子模块" className="subtabs">
          {visibleTabs.map(([id, label]) => (
            <button
              key={id}
              role="tab"
              aria-selected={current === id}
              onClick={() => {
                setTab(id);
                window.location.hash = `weblab/${id}`;
              }}
            >
              {label}
            </button>
          ))}
        </div>
        <span className="text-xs text-muted">交互实验室 · 后续 CocosLab 新仓库模块可直接挂载至此导航栏</span>
      </div>

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
