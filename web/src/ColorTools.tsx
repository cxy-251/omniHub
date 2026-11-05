import { useEffect, useState } from 'react';
import { ChromaLabView } from './color/ChromaLabView';
import './color/colorAtlas.css';
import { GradientAtlasView } from './color/GradientAtlasView';
import { SpectrumAtlasView } from './color/SpectrumAtlasView';
import { notify, store, stored } from './ui';

const SUITE_TABS = [
  { id: 'spectrum', label: 'Spectrum Atlas · 中国色', badge: '3D 地形 / 120 典藏' },
  { id: 'gradient', label: 'Gradient Atlas · 流体渐变', badge: 'Mesh 极光 / 120 预设' },
  { id: 'chroma', label: 'Chroma Lab · 几何配色仪', badge: '色相环 / WCAG' },
] as const;

type SuiteTabId = (typeof SUITE_TABS)[number]['id'];

function copyToClipboard(text: string, label?: string) {
  if (!text) {
    if (label) notify(label);
    return;
  }
  const msg = label ? `已复制 ${label}` : `已复制 ${text}`;
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(
      () => notify(msg),
      () => notify('复制失败'),
    );
    return;
  }
  const area = document.createElement('textarea');
  area.value = text;
  document.body.appendChild(area);
  area.select();
  try {
    document.execCommand('copy');
    notify(msg);
  } catch {
    notify('复制失败');
  }
  area.remove();
}

export default function ColorTools() {
  const [tab, setTab] = useState<SuiteTabId>(() => {
    const saved = stored('omnihub.tools.color.suite', 'spectrum');
    return SUITE_TABS.some(t => t.id === saved) ? (saved as SuiteTabId) : 'spectrum';
  });
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    store('omnihub.tools.color.suite', tab);
  }, [tab]);

  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [fullscreen]);

  return (
    <div className={fullscreen ? 'omnihub-color-suite omnihub-color-suite--fullscreen' : 'omnihub-color-suite'}>
      <header className="color-suite-header">
        <div className="color-suite-tabs" role="tablist" aria-label="Color workbench modes">
          {SUITE_TABS.map(item => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={tab === item.id}
              className="color-suite-tab"
              data-active={tab === item.id}
              onClick={() => setTab(item.id)}
            >
              <span>{item.label}</span>
              <span className="color-suite-tab__badge">{item.badge}</span>
            </button>
          ))}
        </div>

        <button
          type="button"
          className="color-suite-tab"
          style={{ border: '1px solid rgba(255,255,255,0.16)', background: 'rgba(255,255,255,0.06)' }}
          onClick={() => setFullscreen(f => !f)}
        >
          {fullscreen ? '退出沉浸全屏 (Esc)' : '沉浸全屏展厅'}
        </button>
      </header>

      {tab === 'spectrum' ? <SpectrumAtlasView onCopy={copyToClipboard} /> : null}
      {tab === 'gradient' ? <GradientAtlasView onCopy={copyToClipboard} /> : null}
      {tab === 'chroma' ? <ChromaLabView onCopy={copyToClipboard} /> : null}
    </div>
  );
}
